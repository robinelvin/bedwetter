package web

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/approvals/go-approval-tests"
	"github.com/approvals/go-approval-tests/reporters"
)

func TestMain(m *testing.M) {
	approvals.UseReporter(reporters.NewDiffReporter())
	os.Exit(m.Run())
}

func standardScrubbers() approvals.VerifyOptions {
	return approvals.Options().
		WithRegexScrubber(regexp.MustCompile(`/config/zones/\d+/delete`), "/config/zones/N/delete").
		WithRegexScrubber(regexp.MustCompile(`/schedules/\d+/delete`), "/schedules/N/delete").
		WithRegexScrubber(regexp.MustCompile(`name="id" value="\d+"`), `name="id" value="N"`).
		WithRegexScrubber(regexp.MustCompile(`\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}`), "2006-01-02 15:04:05").
		WithRegexScrubber(regexp.MustCompile(`\d{2}:\d{2}:\d{2}`), "HH:MM:SS")
}

func verifyPage(t *testing.T, sv *Server, path string) {
	t.Helper()
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", path, nil)
	sv.router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for %s, got %d", path, w.Code)
	}

	approvals.VerifyString(t, w.Body.String(), standardScrubbers())
}

func verifyPagePost(t *testing.T, sv *Server, path string, form url.Values) {
	t.Helper()
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	sv.router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for %s, got %d", path, w.Code)
	}

	approvals.VerifyString(t, w.Body.String(), standardScrubbers())
}
