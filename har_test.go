package replay

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoadHarFile(t *testing.T) {
	cwd, err := os.Getwd()
	require.NoError(t, err)

	testdata := filepath.Join(cwd, "testdata")
	testfile := filepath.Join(testdata, "postman-echo.com_01-16-2022-13-15-41.har")

	file, err := LoadHarFile(testfile)
	require.NoError(t, err)
	require.Equal(t, "1.2", file.Log.Version)
	require.NotEmpty(t, file.Log.Entries)
	require.Equal(t, "GET", file.Log.Entries[0].Request.Method)
	require.Len(t, file.Log.Entries[0].Request.Headers, 3)
	require.Equal(t, "Host", file.Log.Entries[0].Request.Headers[0].Name)
	require.Equal(t, "postman-echo.com", file.Log.Entries[0].Request.Headers[0].Value)

	require.Len(t, file.Log.Entries[0].Request.QueryString, 2)
	require.Equal(t, "foo1", file.Log.Entries[0].Request.QueryString[0].Name)
	require.Equal(t, "bar1", file.Log.Entries[0].Request.QueryString[0].Value)
	require.Equal(t, "https://postman-echo.com/get?foo1=bar1&foo2=bar2", file.Log.Entries[0].Request.Url)

	require.Len(t, file.Log.Entries[0].Response.Headers, 7)
	require.Equal(t, "Date", file.Log.Entries[0].Response.Headers[0].Name)
	require.Equal(t, "Sun, 16 Jan 2022 20:14:59 GMT", file.Log.Entries[0].Response.Headers[0].Value)
	require.Equal(t, 200, file.Log.Entries[0].Response.Status)
	require.Equal(t, "application/json; charset=utf-8", file.Log.Entries[0].Response.Content.MimeType)

	require.Equal(t, "base64", file.Log.Entries[0].Response.Content.Encoding)
	require.Equal(t, 289, file.Log.Entries[0].Response.Content.Size)

	_, err = base64.StdEncoding.DecodeString(file.Log.Entries[0].Response.Content.Text)
	require.NoError(t, err)

}

func TestLoadPOSTHarFile(t *testing.T) {
	cwd, err := os.Getwd()
	require.NoError(t, err)

	testdata := filepath.Join(cwd, "testdata")
	testfile := filepath.Join(testdata, "postman-echo.com_01-16-2022-13-39-34.har")

	file, err := LoadHarFile(testfile)
	require.NoError(t, err)

	require.Equal(t, "POST", file.Log.Entries[0].Request.Method)
	require.Equal(t, "application/json", file.Log.Entries[0].Request.PostData.MimeType)
	require.Equal(t, "{\"foo\":\"bar\"}\n", file.Log.Entries[0].Request.PostData.Text)

	require.Equal(t, "base64", file.Log.Entries[0].Response.Content.Encoding)
	require.Equal(t, 433, file.Log.Entries[0].Response.Content.Size)

	_, err = base64.StdEncoding.DecodeString(file.Log.Entries[0].Response.Content.Text)
	require.NoError(t, err)
}
