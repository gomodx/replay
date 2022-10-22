package replay

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func newTestRequest() *http.Request {
	return &http.Request{
		Method: "POST",
		URL: &url.URL{
			Scheme: "https",
			Host:   "www.example.com",
			Path:   "/example/path",
		},
		Header:        http.Header{},
		Body:          io.NopCloser(bytes.NewBufferString("")),
		ContentLength: 0,
		Host:          "www.example.com",
	}
}

func TestNewReplayTransport(t *testing.T) {
	cwd, err := os.Getwd()
	require.NoError(t, err)

	testdata := filepath.Join(cwd, "testdata")
	testfile := filepath.Join(testdata, "postman-echo.com_01-16-2022-13-39-34.har")

	rt, err := NewReplayTransport(WithHarFile(testfile))
	require.NoError(t, err)
	require.NotEmpty(t, rt.harFiles)

	file := rt.harFiles[testfile]

	client := rt.NewClient()
	resp, err := client.Do(file.Log.Entries[0].Request.Factory())
	require.NoError(t, err)
	require.NotNil(t, resp)
}

func Test__hashRequest(t *testing.T) {
	r := newTestRequest()

	t.Run("should hash request", func(t *testing.T) {
		hashKey, err := HashRequest(r)
		require.NoError(t, err)
		require.Equal(t, "b54b28b27aee90c17f06e30d05ea0b90cbc1e611702bdf423f01ee8b1f790d86", hashKey)
	})

	t.Run("should hash request with additonal header", func(t *testing.T) {
		r.Header.Add("X-Test-Header", "test header value")
		hashKey, err := HashRequest(r)
		require.NoError(t, err)
		require.Equal(t, "ca809a4c13223bff746882001cab31208094c8d97eaba5f53632053fda492ab6", hashKey)
	})

	t.Run("should be able to redact header and get previous result", func(t *testing.T) {
		hashKey, err := HashRequest(r, func(r *http.Request) {
			r.Header.Del("X-Test-Header")
		})
		require.NoError(t, err)
		require.Equal(t, "b54b28b27aee90c17f06e30d05ea0b90cbc1e611702bdf423f01ee8b1f790d86", hashKey)
	})
}

func TestNewRequestRecorderMiddleware(t *testing.T) {
	cwd, err := os.Getwd()
	require.NoError(t, err)
	logger, _ := zap.NewProduction()
	testdata := filepath.Join(cwd, "testdata")
	expectedReqDir := filepath.Join(testdata, "requests")
	recordDir := filepath.Join(os.TempDir(), "proxy_recorder")
	require.NoError(t, err)

	t.Log("no-req", recordDir)

	tests := map[string]struct {
		method   string
		target   string
		expected string
		body     io.Reader
	}{
		"no body": {
			method:   "GET",
			target:   "https://example.com/v1/resource",
			expected: "8f34da1cdd7de57aba9591986c6a991672044a47a816944bd20eb3a2bd66a2a0.req",
		},
		"json body": {
			method:   "POST",
			target:   "https://example.com/v1/resource/new",
			expected: "eb30fea141f725770a80ef7228996368ca274b2845214fa1e59ca89e3f476aa7.req",
			body:     bytes.NewBufferString(`{"name":"test"}`),
		},
	}

	recorderParams := RequestRecorderMiddlewareParams{
		Dir:            recordDir,
		Enabled:        true,
		Logger:         *logger,
		RequestFilters: nil,
	}
	recorder, err := NewRequestRecorderMiddleware(recorderParams)

	require.NoError(t, err)
	require.DirExists(t, recordDir)

	handler := http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusOK)
	})

	for k, test := range tests {
		t.Run(fmt.Sprintf("%s", k), func(t *testing.T) {
			res := httptest.NewRecorder()
			req := httptest.NewRequest(test.method, test.target, test.body)
			recorder(handler).ServeHTTP(res, req)
			actualFile := filepath.Join(recordDir, test.expected)
			require.FileExists(t, actualFile)

			expectedFile := filepath.Join(expectedReqDir, test.expected)
			expectedBytes, rErr := os.ReadFile(expectedFile)
			require.NoError(t, rErr)

			actualBytes, rErr := os.ReadFile(actualFile)
			require.NoError(t, err)

			require.Equal(t, string(expectedBytes), string(actualBytes))
		})
	}
}
