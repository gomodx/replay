package replay

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"go.uber.org/zap"
	"golang.org/x/oauth2"

	"github.com/pkg/errors"
	"github.com/samber/lo"
	"github.com/sergi/go-diff/diffmatchpatch"
)

type RequestFilter func(r *http.Request)
type ResponseFilter func(w *http.Response)
type ResponseFactory func() *http.Response
type ReplayOption func(transport *ReplayTransport) error

type SingleResponseTransport struct {
	entry Entry
}

type ReplayTransport struct {
	harFiles         map[string]*HarFile
	harResponseCache map[string]Entry
	requestFilters   []RequestFilter
	responseFilters  []ResponseFilter
	debugger         func(key string, request *http.Request) error
}

func (r *ReplayTransport) NewClient() *http.Client {
	return &http.Client{Transport: r}
}

func NewSingleResponseClient(entry Entry) *http.Client {
	return &http.Client{Transport: &SingleResponseTransport{entry}}
}

func (r *SingleResponseTransport) RoundTrip(_ *http.Request) (response *http.Response, err error) {
	response = r.entry.Response.Factory()
	return
}

func (r *ReplayTransport) RoundTrip(request *http.Request) (response *http.Response, err error) {
	for _, filter := range r.requestFilters {
		filter(request)
	}

	hashKey, err := HashRequest(request, r.requestFilters...)
	if err != nil {
		err = errors.Wrap(err, "failed to hash request")
		return
	}

	entry, ok := r.harResponseCache[hashKey]
	if !ok {

		if r.debugger != nil {
			if dbErr := r.debugger(hashKey, request); dbErr != nil {
				err = dbErr
			}
		}

		if err != nil {
			err = errors.Wrap(err, "no cached response for this request")
		} else {
			err = errors.Wrapf(err, "no cached response for this request, try attaching a round trip debugger %s", hashKey)
		}

		return
	}

	response = entry.Response.Factory()
	return
}

func (r *ReplayTransport) cacheEntry(entry Entry) error {
	hashKey, err := HashRequest(entry.Request.Factory(), r.requestFilters...)
	if err != nil {
		return err
	}
	r.harResponseCache[hashKey] = entry
	return nil
}

func RequestToBuff(r *http.Request, filters ...RequestFilter) *bytes.Buffer {
	request := CloneRequestWithBody(r)
	for _, filter := range filters {
		filter(request)
	}

	data, err := httputil.DumpRequestOut(request, r.Body != nil && r.Body != http.NoBody)
	fmt.Println(err)
	// _ = r.Clone(context.Background()).Write(buff)
	return bytes.NewBuffer(data)
}

func HashRequest(r *http.Request, filters ...RequestFilter) (hashKey string, err error) {
	rootHash := sha256.New()
	request := CloneRequestWithBody(r)
	for _, filter := range filters {
		filter(request)
	}

	data, err := httputil.DumpRequestOut(request, r.Body != nil && r.Body != http.NoBody)
	if err != nil {
		return
	}

	if _, err = rootHash.Write(data); err != nil {
		return
	}

	hashKey = hex.EncodeToString(rootHash.Sum(nil))
	return
}

func WithRequestFilter(filter RequestFilter) ReplayOption {
	return func(transport *ReplayTransport) error {
		transport.requestFilters = append(transport.requestFilters, filter)
		return nil
	}
}

func WithHarFile(filename string) ReplayOption {
	return func(transport *ReplayTransport) (err error) {
		transport.harFiles[filename], err = LoadHarFile(filename)
		if err != nil {
			return
		}

		for _, entry := range transport.harFiles[filename].Log.Entries {
			err = transport.cacheEntry(entry)
		}

		return
	}
}

func WithHarDir(dirname string) ReplayOption {
	return func(transport *ReplayTransport) error {
		return filepath.WalkDir(dirname, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}

			if d.IsDir() {
				return nil
			}

			if filepath.Ext(path) == ".har" {
				return WithHarFile(path)(transport)
			}
			return nil
		})
	}
}

func NewReplayTransport(opts ...ReplayOption) (rt *ReplayTransport, err error) {
	rt = &ReplayTransport{
		harFiles:         make(map[string]*HarFile),
		harResponseCache: make(map[string]Entry),
	}
	for _, opt := range opts {
		if err = opt(rt); err != nil {
			return
		}
	}
	return
}

type RequestRecord struct {
	Key  string
	Buff *bytes.Buffer
}

type RequestRecorder struct {
	requests []RequestRecord
	filters  []RequestFilter
	debug    bool
}

func NewRequestRecorder() *RequestRecorder {
	return &RequestRecorder{}
}

func (rdb *RequestRecorder) intercept(r *http.Request) {
	db := RequestRecord{}
	db.Key, _ = HashRequest(r, rdb.filters...)
	db.Buff = RequestToBuff(r, rdb.filters...)
	rdb.requests = append(rdb.requests, db)
}

func WithRoundTripDebugger() ReplayOption {
	differ := diffmatchpatch.New()
	return func(transport *ReplayTransport) error {
		transport.debugger = func(reqKey string, request *http.Request) error {
			var output = new(strings.Builder)
			incoming := RequestToBuff(request, transport.requestFilters...).String()

			for cacheKey, entry := range transport.harResponseCache {
				cached := RequestToBuff(entry.Request.Factory(), transport.requestFilters...).String()
				diff := differ.DiffMain(cached, incoming, true)

				_, _ = fmt.Fprintf(output,
					"\ncached: %s\n%s\nincoming: %s\n%s\ndiff:\n%s\n",
					cacheKey, cached,
					reqKey, incoming,
					differ.DiffPrettyText(diff),
				)
			}
			return errors.New(output.String())
		}
		return nil
	}
}

func WithDelHeaderFilter(key string) ReplayOption {
	return WithRequestFilter(func(r *http.Request) {
		r.Header.Del(key)
	})
}

func WithRequestRecorder(rec *RequestRecorder) ReplayOption {
	return func(transport *ReplayTransport) error {
		rec.filters = append(rec.filters, transport.requestFilters...)
		transport.requestFilters = append(transport.requestFilters, rec.intercept)
		return nil
	}
}

func WithProxyTransport(client *http.Client, proxyURL *url.URL) *http.Client {
	client.Transport.(*oauth2.Transport).Base = &http.Transport{
		Proxy: http.ProxyURL(proxyURL),
		DialContext: (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
	return client
}

type RequestRecorderMiddlewareParams struct {
	Dir            string
	RequestFilters []RequestFilter
	Logger         zap.Logger
	Enabled        bool
	URLMapper      func(r *http.Request) url.URL
}

func NewRequestRecorderMiddleware(params RequestRecorderMiddlewareParams) (func(next http.Handler) http.Handler, error) {
	if params.Dir == "" {
		params.Dir = filepath.Join(os.TempDir(), "proxy-recorder", "requests")
	}
	if err := os.MkdirAll(params.Dir, 0700); err != nil {
		return nil, errors.Wrap(err, "failed to initialize request recorder directory")
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			defer next.ServeHTTP(writer, request)
			if !params.Enabled {
				return
			}

			reqCopy := CloneRequestWithBody(request)
			if params.URLMapper != nil {
				reqCopy.URL = lo.ToPtr(params.URLMapper(reqCopy))
			}

			checksum, err := HashRequest(CloneRequestWithBody(reqCopy), params.RequestFilters...)
			if err != nil {
				params.Logger.Error("failed to hash request", zap.Error(err))
				return
			}

			filename := filepath.Join(params.Dir, checksum+".req")
			data := RequestToBuff(CloneRequestWithBody(reqCopy), params.RequestFilters...)
			if err = os.WriteFile(filename, data.Bytes(), 0744); err != nil {
				params.Logger.Error("failed to write request data", zap.Error(err), zap.String("file", filename))
				return
			}
		})
	}, nil
}
