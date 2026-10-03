// Command speccoverage checks the SMTPfast OpenAPI spec against
// spec-coverage.json and lists the operations that the file does not
// classify as covered by a resource or ignored on purpose.
//
// Run it from the repository root:
//
//	go run ./tools/speccoverage
//
// It exits 0 whatever it finds, and non-zero only when it cannot fetch or
// parse the spec or the coverage file. It is not part of the provider binary.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

const (
	defaultSpecURL = "https://smtpfa.st/api/v1/openapi.json"
	readmeURL      = "https://github.com/smtpfast/terraform-provider-smtpfast#spec-coverage"
	maxSpecBytes   = 10 << 20
)

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "speccoverage:", err)
		os.Exit(1)
	}
}

func run(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("speccoverage", flag.ContinueOnError)
	specSrc := fs.String("spec", defaultSpecURL, "OpenAPI spec `URL or file`")
	coveragePath := fs.String("coverage", "spec-coverage.json", "coverage `file`")
	format := fs.String("format", "text", "text, or markdown for the tracking issue body (prints nothing when there is nothing to report)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *format != "text" && *format != "markdown" {
		return fmt.Errorf("unknown -format %q (want text or markdown)", *format)
	}

	covData, err := os.ReadFile(*coveragePath)
	if err != nil {
		return err
	}
	cov, err := LoadCoverage(covData)
	if err != nil {
		return err
	}
	specData, err := readSpec(*specSrc)
	if err != nil {
		return err
	}
	ops, err := ParseSpec(specData)
	if err != nil {
		return err
	}

	rep := cov.Check(ops)
	var text string
	if *format == "markdown" {
		text = formatMarkdown(rep, *coveragePath)
	} else {
		text = formatText(rep, *specSrc, *coveragePath)
	}
	_, err = io.WriteString(out, text)
	return err
}

func readSpec(src string) ([]byte, error) {
	if !strings.HasPrefix(src, "https://") && !strings.HasPrefix(src, "http://") {
		return os.ReadFile(src)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, src, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "terraform-provider-smtpfast-speccoverage")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch spec: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch spec: %s returned %s", src, resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxSpecBytes+1))
	if err != nil {
		return nil, fmt.Errorf("fetch spec: %w", err)
	}
	if len(data) > maxSpecBytes {
		return nil, errors.New("fetch spec: response is larger than 10 MB")
	}
	return data, nil
}

func formatText(rep Report, specSrc, coveragePath string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s: %d operations, %d covered, %d ignored, %d not classified in %s.\n",
		specSrc, rep.Total, rep.Covered, rep.Ignored, len(rep.Unclassified), coveragePath)
	if len(rep.Unclassified) > 0 {
		width := 0
		for _, op := range rep.Unclassified {
			width = max(width, len(op.Path))
		}
		b.WriteString("\nNot classified:\n")
		for _, op := range rep.Unclassified {
			fmt.Fprintf(&b, "  %-7s %-*s  %s\n", op.Method, width, op.Path, op.OperationID)
		}
	}
	if len(rep.UnusedRules) > 0 {
		b.WriteString("\nRules that match no operation:\n")
		for _, r := range rep.UnusedRules {
			fmt.Fprintf(&b, "  %s\n", r)
		}
	}
	return b.String()
}

// formatMarkdown returns the tracking issue body. It depends only on the
// lists, not on totals, so the body changes only when the lists do. It is
// empty when both lists are empty.
func formatMarkdown(rep Report, coveragePath string) string {
	if len(rep.Unclassified) == 0 && len(rep.UnusedRules) == 0 {
		return ""
	}
	var b strings.Builder
	if n := len(rep.Unclassified); n > 0 {
		noun := "operations are"
		if n == 1 {
			noun = "operation is"
		}
		fmt.Fprintf(&b, "%d SMTPfast API %s not classified in `%s`. Each is either a gap the provider could fill, or an endpoint to add to the file as covered or ignored.\n\n", n, noun, coveragePath)
		b.WriteString("| Method | Path | Operation | Summary |\n| --- | --- | --- | --- |\n")
		for _, op := range rep.Unclassified {
			fmt.Fprintf(&b, "| %s | `%s` | `%s` | %s |\n", op.Method, op.Path, op.OperationID, tableCell(op.Summary))
		}
		b.WriteString("\n")
	}
	if len(rep.UnusedRules) > 0 {
		fmt.Fprintf(&b, "These rules in `%s` match no operation in the spec. A covered rule here can mean the provider calls an endpoint the API no longer has.\n\n", coveragePath)
		for _, r := range rep.UnusedRules {
			fmt.Fprintf(&b, "- `%s`\n", r)
		}
		b.WriteString("\n")
	}
	fmt.Fprintf(&b, "This issue is kept up to date by the daily spec coverage workflow and closes when nothing is left. See [how to classify an endpoint](%s).\n", readmeURL)
	return b.String()
}

func tableCell(s string) string {
	s = strings.ReplaceAll(s, "|", `\|`)
	return strings.Join(strings.Fields(s), " ")
}
