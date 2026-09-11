// Package youtrack is a read-only client for the YouTrack REST API.
//
// Hard invariant: only GET requests live here. See CLAUDE.md.
package youtrack

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// Field specs sent to YouTrack. The API returns nothing unless asked, so these
// strings are the real schema of everything below.
const (
	fieldsSavedQuery = "id,name,query"

	// Custom field values come in a dozen shapes; we ask for every key the
	// generic renderer knows how to read and let the API drop what does not
	// apply. $type is what keeps date/period rendering honest.
	// The value keys are named on their own because two other requests read
	// the same shapes: editable fields at their own endpoint, and a history
	// entry under added and removed.
	valueKeys    = "($type,name,fullName,login,presentation,text,minutes)"
	fieldsValue  = "value" + valueKeys
	fieldsCustom = "customFields(name,$type," + fieldsValue + ")"

	fieldsIssueList = "idReadable,summary,created,updated,resolved," + fieldsCustom

	fieldsAttachment = "attachments(name,url,size,mimeType)"
	fieldsLink       = "links(direction,linkType(name,sourceToTarget,targetToSource),issues(idReadable,summary))"

	fieldsIssue = fieldsIssueList + ",description,reporter(fullName,login)," +
		fieldsAttachment + "," + fieldsLink

	fieldsComment = "id,text,created,author(fullName,login)," + fieldsAttachment

	fieldsChange = "timestamp,author(fullName,login),field(presentation,customField(fieldType(id)))," +
		"added" + valueKeys + ",removed" + valueKeys
)

// TLS controls how one provider's certificate is verified.
type TLS struct {
	// CAFile is a PEM bundle trusted on top of the system roots.
	CAFile string
	// Insecure disables verification entirely. It is a real downgrade — the
	// token goes to whoever answers — so it is never a global default and the
	// UI keeps it visible while it is on.
	Insecure bool
}

// Client talks to one YouTrack instance.
type Client struct {
	base  string // host root, no trailing slash
	token string
	http  *http.Client
}

// New builds a client for base (e.g. https://acme.youtrack.cloud).
func New(base, token string, t TLS) (*Client, error) {
	// Cloned rather than built from scratch so proxy settings, keep-alives and
	// HTTP/2 survive — corporate networks need all three.
	tr := http.DefaultTransport.(*http.Transport).Clone()
	cfg := &tls.Config{MinVersion: tls.VersionTLS12}

	switch {
	case t.Insecure:
		cfg.InsecureSkipVerify = true
	case t.CAFile != "":
		pem, err := os.ReadFile(t.CAFile)
		if err != nil {
			return nil, fmt.Errorf("ca_file: %w", err)
		}
		pool, err := x509.SystemCertPool()
		if err != nil {
			pool = x509.NewCertPool()
		}
		if !pool.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("ca_file %s: no PEM certificate found", t.CAFile)
		}
		cfg.RootCAs = pool
	}
	tr.TLSClientConfig = cfg

	return &Client{
		base:  strings.TrimRight(base, "/"),
		token: token,
		// Long enough for a slow instance over VPN, short enough that a
		// blocked request does not leave the TUI showing a spinner forever.
		http: &http.Client{Timeout: 30 * time.Second, Transport: tr},
	}, nil
}

// IsCertError reports whether err is the server's certificate failing to
// verify — the one failure the UI can offer to work around.
func IsCertError(err error) bool {
	var (
		verify   *tls.CertificateVerificationError
		unknown  x509.UnknownAuthorityError
		invalid  x509.CertificateInvalidError
		hostname x509.HostnameError
	)
	return errors.As(err, &verify) || errors.As(err, &unknown) ||
		errors.As(err, &invalid) || errors.As(err, &hostname)
}

// IssueURL is the human-facing page for an issue, used for OSC 8 hyperlinks.
func (c *Client) IssueURL(id string) string { return c.base + "/issue/" + id }

// AbsURL turns the relative, pre-signed URL YouTrack returns for attachments
// into something a terminal can hand to a browser.
func (c *Client) AbsURL(rel string) string {
	if rel == "" || strings.HasPrefix(rel, "http") {
		return rel
	}
	return c.base + rel
}

// SavedQueries returns the saved searches visible to the token's owner.
func (c *Client) SavedQueries(ctx context.Context) ([]SavedQuery, error) {
	var out []SavedQuery
	err := c.get(ctx, "/savedQueries", url.Values{"fields": {fieldsSavedQuery}}, &out)
	return out, err
}

// Issues runs a YouTrack query and returns at most top issues, skipping the
// first skip of them. A short page means there is nothing after it.
func (c *Client) Issues(ctx context.Context, query string, skip, top int) ([]Issue, error) {
	var out []Issue
	err := c.get(ctx, "/issues", url.Values{
		"query":  {query},
		"fields": {fieldsIssueList},
		"$skip":  {strconv.Itoa(skip)},
		"$top":   {strconv.Itoa(top)},
	}, &out)
	return out, err
}

// Issue returns one issue with description, attachments and links.
func (c *Client) Issue(ctx context.Context, id string) (*Issue, error) {
	var out Issue
	err := c.get(ctx, "/issues/"+url.PathEscape(id), url.Values{"fields": {fieldsIssue}}, &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// Comments returns an issue's comments, oldest first.
func (c *Client) Comments(ctx context.Context, id string) ([]Comment, error) {
	var out []Comment
	err := c.get(ctx, "/issues/"+url.PathEscape(id)+"/comments",
		url.Values{"fields": {fieldsComment}}, &out)
	return out, err
}

// History returns the custom field edits made to an issue, oldest first.
func (c *Client) History(ctx context.Context, id string) ([]Change, error) {
	var out []Change
	err := c.get(ctx, "/issues/"+url.PathEscape(id)+"/activities", url.Values{
		"categories": {"CustomFieldCategory"},
		"fields":     {fieldsChange},
	}, &out)
	return out, err
}

func (c *Client) get(ctx context.Context, path string, params url.Values, out any) error {
	endpoint := c.base + "/api" + path + "?" + params.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		// url.Error carries the full request URL, fields spec and all, which
		// buries the actual cause. Unwrap it and name the path instead. It
		// never carries headers, so the token cannot leak either way.
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err
		}
		return fmt.Errorf("GET %s: %w", path, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return &APIError{Status: resp.StatusCode, Body: readSnippet(resp.Body)}
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// APIError is a non-200 response from YouTrack.
type APIError struct {
	Status int
	Body   string
}

func (e *APIError) Error() string {
	msg := http.StatusText(e.Status)
	if e.Body != "" {
		msg += ": " + e.Body
	}
	if e.Status == http.StatusUnauthorized || e.Status == http.StatusForbidden {
		msg += " (invalid token, or missing permission)"
	}
	return fmt.Sprintf("HTTP %d %s", e.Status, msg)
}

func readSnippet(r io.Reader) string {
	b, _ := io.ReadAll(io.LimitReader(r, 512))
	var e struct {
		Description string `json:"error_description"`
		Error       string `json:"error"`
	}
	if json.Unmarshal(b, &e) == nil && e.Description != "" {
		return e.Description
	}
	return strings.TrimSpace(string(b))
}
