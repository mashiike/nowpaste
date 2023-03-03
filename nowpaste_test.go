package nowpaste

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"os"
	"testing"

	"github.com/sebdah/goldie/v2"
	"github.com/slack-go/slack"
)

type mockSlackServer func(w http.ResponseWriter, r *http.Request)

func (f mockSlackServer) Do(r *http.Request) (*http.Response, error) {
	w := httptest.NewRecorder()
	switch {
	case r.URL.Path == "/api/auth.test":
		w.WriteHeader(http.StatusOK)
		fp, err := os.Open("testdata/example_auth_test_response.json")
		if err != nil {
			return nil, fmt.Errorf("example_auth_test_response.json: %w", err)
		}
		defer fp.Close()
		io.Copy(w, fp)
	default:
		f(w, r)
	}
	return w.Result(), nil
}

type postRootTestCase struct {
	name                  string
	slackResponseHeaders  map[string]string
	slackResponseBodyFile string
	slackResponseStatus   int
	requestHeaders        map[string]string
	newRequestBody        func(t *testing.T) io.Reader
}

func (c postRootTestCase) Run(t *testing.T, g *goldie.Goldie, middlewares ...func(func(w http.ResponseWriter, r *http.Request)) func(w http.ResponseWriter, r *http.Request)) {
	f := func(w http.ResponseWriter, r *http.Request) {
		dump, err := httputil.DumpRequestOut(r, true)
		if err != nil {
			t.Error("request dump error:", err)
			t.FailNow()
		}
		g.Assert(t, c.name, dump)
		fp, err := os.Open(c.slackResponseBodyFile)
		if err != nil {
			t.Error("can not open response data:", err)
			t.FailNow()
		}
		defer fp.Close()
		for key, value := range c.slackResponseHeaders {
			w.Header().Set(key, value)
		}
		w.WriteHeader(c.slackResponseStatus)
		io.Copy(w, fp)
	}
	for _, m := range middlewares {
		f = m(f)
	}
	client := newWithClient(slack.New("dummy_token", slack.OptionHTTPClient(
		mockSlackServer(f),
	)))

	req := httptest.NewRequest(http.MethodPost, "/", c.newRequestBody(t))
	for key, value := range c.requestHeaders {
		req.Header.Add(key, value)
	}
	w := httptest.NewRecorder()
	client.ServeHTTP(w, req)
	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Error("http status not ok ", resp)
	}
}

var postRootTestCases []postRootTestCase = []postRootTestCase{
	{
		name:                  "short",
		slackResponseBodyFile: "testdata/example_chat_post_message_response.json",
		slackResponseStatus:   http.StatusOK,
		requestHeaders: map[string]string{
			"Content-Type": "application/json",
		},
		newRequestBody: func(t *testing.T) io.Reader {
			body, _ := json.Marshal(map[string]string{
				"channel": "#test",
				"text":    "this is test message",
			})
			return bytes.NewReader(body)
		},
	},
	{
		name:                  "many_lines",
		slackResponseBodyFile: "testdata/example_file_upload_response.json",
		slackResponseStatus:   http.StatusOK,
		requestHeaders: map[string]string{
			"Content-Type": "application/json",
		},
		newRequestBody: func(t *testing.T) io.Reader {
			body, _ := json.Marshal(map[string]string{
				"channel": "#test",
				"text":    "this is test message\nthis is test message\nthis is test message\nthis is test message\nthis is test message\nthis is test message\n",
			})
			return bytes.NewReader(body)
		},
	},
}

func TestPostRootSuccess(t *testing.T) {
	g := goldie.New(t,
		goldie.WithFixtureDir("testdata/post_root_success/"),
	)
	for _, c := range postRootTestCases {
		t.Run(c.name, func(t *testing.T) {
			c.Run(t, g)
		})
	}
}

func TestPostRootRetryOnce(t *testing.T) {
	g := goldie.New(t,
		goldie.WithFixtureDir("testdata/post_root_retry_once/"),
	)
	for _, c := range postRootTestCases {
		t.Run(c.name, func(t *testing.T) {
			i := 0
			c.Run(t, g, func(next func(w http.ResponseWriter, r *http.Request)) func(w http.ResponseWriter, r *http.Request) {
				return func(w http.ResponseWriter, r *http.Request) {
					if i == 0 {
						w.Header().Set("Retry-After", "1")
						w.WriteHeader(http.StatusTooManyRequests)
						i++
					}
					next(w, r)
				}
			})
		})
	}
}
