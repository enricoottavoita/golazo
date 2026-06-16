package reddit

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestPublicJSONFetcherSearchTimestampWindow verifies that the Reddit search
// URL spans the matcher's accepted post-date window (-24h .. +48h around the
// match time) so candidate posts in that range are not pre-filtered out by
// the server-side timestamp query.
func TestPublicJSONFetcherSearchTimestampWindow(t *testing.T) {
	matchTime := time.Date(2025, 11, 10, 16, 0, 0, 0, time.UTC)
	wantStart := matchTime.Add(-24 * time.Hour).Unix()
	wantEnd := matchTime.Add(48 * time.Hour).Unix()

	var capturedURL *url.URL
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedURL = r.URL
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"data":{"children":[]}}`)
	}))
	defer server.Close()

	// We need the fetcher to hit the test server instead of www.reddit.com.
	// PublicJSONFetcher hardcodes the URL, so use a transport that rewrites
	// the host.
	fetcher := NewPublicJSONFetcher()
	fetcher.httpClient.Transport = &rewriteTransport{target: server.URL}

	_, err := fetcher.Search("australia turkey 27", 10, matchTime, "relevance")
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	if capturedURL == nil {
		t.Fatal("test server did not receive a request")
	}

	q := capturedURL.Query().Get("q")
	want := fmt.Sprintf("timestamp:%d..%d", wantStart, wantEnd)
	if !strings.Contains(q, want) {
		t.Errorf("query missing timestamp window: q=%q, want substring %q", q, want)
	}
}

// rewriteTransport redirects any outbound request to the configured target
// host while preserving the original path and query string.
type rewriteTransport struct {
	target string
}

func (rt *rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	targetURL, err := url.Parse(rt.target)
	if err != nil {
		return nil, err
	}
	req.URL.Scheme = targetURL.Scheme
	req.URL.Host = targetURL.Host
	req.Host = targetURL.Host
	return http.DefaultTransport.RoundTrip(req)
}

// TestPublicJSONFetcherSearchHonorsSortParam pins the sort param so future
// refactors don't silently change ranking behavior.
func TestPublicJSONFetcherSearchHonorsSortParam(t *testing.T) {
	var capturedURL *url.URL
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedURL = r.URL
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"data":{"children":[]}}`)
	}))
	defer server.Close()

	fetcher := NewPublicJSONFetcher()
	fetcher.httpClient.Transport = &rewriteTransport{target: server.URL}

	_, err := fetcher.Search("q", 10, time.Now(), "top")
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	if got := capturedURL.Query().Get("sort"); got != "top" {
		t.Errorf("sort param: got %q, want %q", got, "top")
	}
	if got := capturedURL.Query().Get("limit"); got != strconv.Itoa(10) {
		t.Errorf("limit param: got %q, want %q", got, strconv.Itoa(10))
	}
}

// TestSearchReturnsErrBlockedOn403 pins the typed-error contract for HTTP 403
// responses from Reddit's edge. The queue worker introduced in the goal-link
// rework uses errors.Is(err, ErrBlocked) to enter cooldown; sniffing on
// response body substrings was the previous (fragile) detection mechanism.
func TestSearchReturnsErrBlockedOn403(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(w, `<html>You've been blocked by network security.</html>`)
	}))
	defer server.Close()

	fetcher := NewPublicJSONFetcher()
	fetcher.httpClient.Transport = &rewriteTransport{target: server.URL}

	_, err := fetcher.Search("anything", 5, time.Now(), "relevance")
	if err == nil {
		t.Fatal("Search returned nil error for 403 response")
	}
	if !errors.Is(err, ErrBlocked) {
		t.Fatalf("Search error %v is not ErrBlocked", err)
	}
}

// recordingFetcher captures queries passed to Search so tests can assert on
// the exact query string searchForGoalOnce constructs (vs. e2e-asserting via
// httptest, which would re-test url-escaping). Returns whatever results are
// pre-loaded.
type recordingFetcher struct {
	queries []string
	results []SearchResult
	err     error
}

func (r *recordingFetcher) Search(query string, _ int, _ time.Time, _ string) ([]SearchResult, error) {
	r.queries = append(r.queries, query)
	if r.err != nil {
		return nil, r.err
	}
	return r.results, nil
}

// TestSearchForGoalOnceQueryFormat pins the single-query format produced by
// buildGoalQuery / searchForGoalOnce: "<home> <hScore> <aScore> <away>
// <scorerLast>", with the scorer token omitted when ScorerName is empty.
// Asserts exactly one fetcher.Search call (strategies 2 and 3 are gone).
func TestSearchForGoalOnceQueryFormat(t *testing.T) {
	cases := []struct {
		name      string
		goal      GoalInfo
		wantQuery string
	}{
		{
			name: "scorer present uses last token",
			goal: GoalInfo{
				MatchID:    4667791,
				HomeTeam:   "Iran",
				AwayTeam:   "New Zealand",
				HomeScore:  0,
				AwayScore:  1,
				ScorerName: "Elijah Just",
				Minute:     7,
				IsHomeTeam: false,
				MatchTime:  time.Now(),
			},
			wantQuery: "Iran 0 1 New Zealand Just",
		},
		{
			name: "empty scorer falls back to teams + score",
			goal: GoalInfo{
				MatchID:    1,
				HomeTeam:   "Iran",
				AwayTeam:   "New Zealand",
				HomeScore:  1,
				AwayScore:  1,
				ScorerName: "",
				Minute:     50,
				MatchTime:  time.Now(),
			},
			wantQuery: "Iran 1 1 New Zealand",
		},
		{
			name: "scorer with diacritics is folded",
			goal: GoalInfo{
				MatchID:    2,
				HomeTeam:   "Liverpool",
				AwayTeam:   "Wolves",
				HomeScore:  2,
				AwayScore:  0,
				ScorerName: "Darwin Núñez",
				Minute:     12,
				MatchTime:  time.Now(),
			},
			wantQuery: "Liverpool 2 0 Wolves Nunez",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fetcher := &recordingFetcher{}
			client := NewClientWithFetcher(fetcher, &GoalLinkCache{links: make(map[string]GoalLink)})

			_, err := client.searchForGoalOnce(tc.goal)
			if err != nil {
				t.Fatalf("searchForGoalOnce returned err: %v", err)
			}
			if len(fetcher.queries) != 1 {
				t.Fatalf("expected exactly 1 fetcher.Search call (single-strategy), got %d: %v",
					len(fetcher.queries), fetcher.queries)
			}
			if fetcher.queries[0] != tc.wantQuery {
				t.Errorf("query mismatch:\n  got  %q\n  want %q", fetcher.queries[0], tc.wantQuery)
			}
		})
	}
}
