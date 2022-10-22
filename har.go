package replay

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/pkg/errors"
)

type HarFile struct {
	Log Log `json:"log"`
}

type Creator struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type Log struct {
	Version string  `json:"version"`
	Creator Creator `json:"creator"`
	Entries []Entry `json:"entries"`
}

type Timing struct {
	Connect int `json:"connect"`
	Send    int `json:"send"`
	Dns     int `json:"dns"`
	Ssl     int `json:"ssl"`
	Wait    int `json:"wait"`
	Blocked int `json:"blocked"`
	Receive int `json:"receive"`
}

type QueryParams []QueryParam
type QueryParam struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

func (q QueryParams) ToURLValues() (v url.Values) {
	v = make(url.Values)
	for _, param := range q {
		v.Add(param.Name, param.Value)
	}
	return
}

type Headers []Header
type Header struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

func (h Headers) ToHTTPHeader() (ht http.Header) {
	ht = make(http.Header)
	for _, header := range h {
		ht.Add(header.Name, header.Value)
	}
	return
}

type PostData struct {
	Params   []any  `json:"params"`
	Text     string `json:"text"`
	MimeType string `json:"mimeType"`
}

type Request struct {
	Method      string        `json:"method"`
	BodySize    int           `json:"bodySize"`
	HeadersSize int           `json:"headersSize"`
	PostData    PostData      `json:"postData"`
	Cookies     []interface{} `json:"cookies"`
	Headers     Headers       `json:"headers"`
	QueryString QueryParams   `json:"queryString"`
	HttpVersion string        `json:"httpVersion"`
	Url         string        `json:"url"`
}

func (r Request) Factory() *http.Request {
	headers := r.Headers.ToHTTPHeader()
	u, _ := url.Parse(r.Url)
	major, minor, _ := http.ParseHTTPVersion(r.HttpVersion)
	getBody := func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader([]byte(r.PostData.Text))), nil
	}
	body, _ := getBody()
	return &http.Request{
		Method:        r.Method,
		URL:           u,
		Header:        headers,
		Proto:         r.HttpVersion,
		ProtoMajor:    major,
		ProtoMinor:    minor,
		Body:          body,
		GetBody:       getBody,
		ContentLength: int64(len([]byte(r.PostData.Text))),
		Host:          headers.Get("Host"),
	}
}

type ContentType struct {
	Size     int    `json:"size"`
	MimeType string `json:"mimeType"`
	Encoding string `json:"encoding"`
	Text     string `json:"text"`
}

type Response struct {
	Status      int         `json:"status"`
	Content     ContentType `json:"content"`
	BodySize    int         `json:"bodySize"`
	HeadersSize int         `json:"headersSize"`
	Cookies     []any       `json:"cookies"`
	StatusText  string      `json:"statusText"`
	Headers     Headers     `json:"headers"`
	HttpVersion string      `json:"httpVersion"`
	RedirectURL string      `json:"redirectURL"`
}

func (r Response) Factory() *http.Response {
	body, _ := base64.StdEncoding.DecodeString(r.Content.Text)
	major, minor, _ := http.ParseHTTPVersion(r.HttpVersion)
	return &http.Response{
		Status:        r.StatusText,
		StatusCode:    r.Status,
		Proto:         r.HttpVersion,
		ProtoMajor:    major,
		ProtoMinor:    minor,
		Header:        r.Headers.ToHTTPHeader(),
		Body:          io.NopCloser(bytes.NewBuffer(body)),
		ContentLength: int64(len(body)),
	}
}

type Entry struct {
	Time              float64   `json:"time"`
	IsHTTPS           bool      `json:"_isHTTPS"`
	WebSocketMessages any       `json:"_webSocketMessages"`
	RemoteDeviceIP    string    `json:"_remoteDeviceIP"`
	Timings           Timing    `json:"timings"`
	ServerAddress     string    `json:"_serverAddress"`
	IsIntercepted     bool      `json:"_isIntercepted"`
	Id                string    `json:"_id"`
	ServerIPAddress   string    `json:"serverIPAddress"`
	Name              string    `json:"_name"`
	ClientAddress     string    `json:"_clientAddress"`
	ClientBundlePath  any       `json:"_clientBundlePath"`
	Request           Request   `json:"request"`
	ServerPort        int       `json:"_serverPort"`
	ClientName        any       `json:"_clientName"`
	ClientPort        int       `json:"_clientPort"`
	Response          Response  `json:"response"`
	Comment           string    `json:"comment"`
	StartedDateTime   time.Time `json:"startedDateTime"`
}

func LoadHarFile(harFilePath string) (har *HarFile, err error) {
	har = new(HarFile)
	f, err := os.Open(harFilePath)
	if err != nil {
		err = errors.Wrapf(err, "failed to open %s", harFilePath)
		return
	}
	defer func(f *os.File) {
		_ = f.Close()
	}(f)

	if err = json.NewDecoder(f).Decode(&har); err != nil {
		err = errors.Wrap(err, "failed to decode HAR file")
	}

	return
}

func CloneRequestWithBody(r *http.Request) *http.Request {
	var b bytes.Buffer
	var r2 = r.Clone(context.Background())

	if r.Body == nil || r.Body == http.NoBody {
		return r2
	}

	_, _ = b.ReadFrom(r.Body)
	r.Body = io.NopCloser(&b)
	r2.Body = io.NopCloser(bytes.NewReader(b.Bytes()))
	return r2
}

func CloneResponseWithBody(r *http.Response) *http.Response {
	var b bytes.Buffer
	var r2 = new(http.Response)
	*r2 = *r

	if r.Body == nil || r.Body == http.NoBody {
		return r2
	}

	_, _ = b.ReadFrom(r.Body)
	r.Body = io.NopCloser(&b)
	r2.Body = io.NopCloser(bytes.NewReader(b.Bytes()))
	return r2
}
