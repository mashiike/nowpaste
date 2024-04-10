package nowpaste

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"strings"
	"testing"

	"github.com/sebdah/goldie/v2"
	"github.com/slack-go/slack"
)

type mockClient func(w http.ResponseWriter, r *http.Request)

func (f mockClient) Do(r *http.Request) (*http.Response, error) {
	w := httptest.NewRecorder()
	f(w, r)
	return w.Result(), nil
}

type statusCodeRecoder struct {
	staus int
	http.ResponseWriter
}

func (w *statusCodeRecoder) WriteHeader(statusCode int) {
	w.staus = statusCode
	w.ResponseWriter.WriteHeader(statusCode)
}

type postRootTestCase struct {
	name           string
	requestHeaders map[string]string
	newRequestBody func(t *testing.T) io.Reader
	expectedStatus int
}

//go:embed testdata/example_auth_test_response.json
var authTestRespopnse string

//go:embed testdata/example_chat_post_message_response.json
var chatPostMessageResponse string

//go:embed testdata/example_files_get_upload_url_external_response.json
var filesGetUploadURLExtendedResponse string

//go:embed testdata/example_files_complete_upload_external_response.json
var filesCompleteUploadExternalResponse string

func (c postRootTestCase) Run(t *testing.T, g *goldie.Goldie, middlewares ...func(func(w http.ResponseWriter, r *http.Request)) func(w http.ResponseWriter, r *http.Request)) {
	var apiCallLog bytes.Buffer
	defer func() {
		g.Assert(t, c.name, apiCallLog.Bytes())
	}()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/auth.test", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, authTestRespopnse)
	})
	mux.HandleFunc("/api/chat.postMessage", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, chatPostMessageResponse)
	})
	mux.HandleFunc("/api/files.getUploadURLExternal", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, filesGetUploadURLExtendedResponse)
	})
	mux.HandleFunc("/upload/v1/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/api/files.completeUploadExternal", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, filesCompleteUploadExternalResponse)
	})
	var f http.HandlerFunc
	f = mux.ServeHTTP
	for _, m := range middlewares {
		f = m(f)
	}
	client := newWithClient(slack.New("dummy_token", slack.OptionHTTPClient(
		mockClient(func(w http.ResponseWriter, r *http.Request) {
			dump, err := httputil.DumpRequestOut(r, true)
			if err != nil {
				t.Error("request dump error:", err)
				t.FailNow()
			}
			contentType := r.Header.Get("Content-Type")
			if strings.HasPrefix(contentType, "multipart/form-data") {
				parts := strings.Split(contentType, ";")
				for _, part := range parts {
					if strings.HasPrefix(part, " boundary=") {
						boundary := strings.Trim(part[len(" boundary="):], " ")
						dump = bytes.ReplaceAll(dump, []byte(boundary), []byte("000000000000000000000000000000000000000000000000000000000000"))
					}
				}
			}
			fmt.Fprintf(&apiCallLog, "--- request --\n%s\n", dump)
			recoder := &statusCodeRecoder{ResponseWriter: w}
			f(recoder, r)
			fmt.Fprintf(&apiCallLog, "--- response status ---\n%d %s\n=====================\n", recoder.staus, http.StatusText(recoder.staus))
		}),
	)))

	req := httptest.NewRequest(http.MethodPost, "/", c.newRequestBody(t))
	for key, value := range c.requestHeaders {
		req.Header.Add(key, value)
	}
	w := httptest.NewRecorder()
	client.ServeHTTP(w, req)
	resp := w.Result()
	if resp.StatusCode != c.expectedStatus {
		t.Error("http status unexpected ", resp)
	}
}

var postRootTestCases []postRootTestCase = []postRootTestCase{
	{
		name: "short",
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
		expectedStatus: http.StatusOK,
	},
	{
		name: "many_lines",
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
		expectedStatus: http.StatusOK,
	},
}

func TestPostRootSuccess(t *testing.T) {
	g := goldie.New(t,
		goldie.WithFixtureDir("testdata/post_root_success/"),
	)
	for _, c := range postRootTestCases {
		t.Run(c.name, func(t *testing.T) {
			log.Println("===== start test case", c.name, "=====")
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
						return
					}
					next(w, r)
				}
			})
		})
	}
}
