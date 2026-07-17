// Package email: templates.go renders outbound emails into HTML using a single
// embedded wrapper template. The locale-specific subject/body/greeting/signature
// are produced by the caller via i18n.T; this file only structures them into a
// consistent, brand-styled HTML envelope.
package email

import (
	"bytes"
	"embed"
	"fmt"
	"html/template"
	"sync"
)

//go:embed templates/*.html
var templateFS embed.FS

var (
	templateOnce sync.Once
	rootTemplate *template.Template
	errParse     error
)

// RenderedEmail carries the localised strings ready to drop into the HTML
// wrapper. The caller fills it from i18n.T + the contextual link.
type RenderedEmail struct {
	Greeting   string
	Body       string // already includes any {{.Link}} interpolation
	Signature  string
	CTALabel   string // optional button label
	CTALink    string // optional button URL
	LocaleHint string // optional Content-Language hint (e.g. "en")
}

// wrapperData matches the embedded wrapper template.
type wrapperData struct {
	Greeting   string
	Body       template.HTML
	Signature  string
	CTALabel   string
	CTALink    string
	LocaleHint string
}

// loadTemplate parses the embedded wrapper once per process. The body is
// pre-rendered text and is injected as safe HTML (it came from our own trusted
// i18n bundles, not user input).
func loadTemplate() (*template.Template, error) {
	templateOnce.Do(func() {
		rootTemplate, errParse = template.ParseFS(templateFS, "templates/body.html")
	})
	return rootTemplate, errParse
}

// RenderHTML returns the HTML body for an outbound email given the localised
// parts. The returned string is what SMTPSender ships as the message body.
func RenderHTML(r RenderedEmail) (string, error) {
	tpl, err := loadTemplate()
	if err != nil {
		return "", fmt.Errorf("email: load template: %w", err)
	}
	data := wrapperData{
		Greeting:   r.Greeting,
		Body:       template.HTML(r.Body),
		Signature:  r.Signature,
		CTALabel:   r.CTALabel,
		CTALink:    r.CTALink,
		LocaleHint: r.LocaleHint,
	}
	var buf bytes.Buffer
	if err := tpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("email: render template: %w", err)
	}
	return buf.String(), nil
}
