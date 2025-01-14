package httpx_test

import (
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/nyaruka/gocommon/dates"
	"github.com/nyaruka/gocommon/httpx"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestHTTPServer(port int) *httptest.Server {
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()

		var contentType string
		var data []byte

		cmd := r.URL.Query().Get("cmd")
		switch cmd {
		case "success":
			contentType = "text/plain; charset=utf-8"
			data = []byte(`{ "ok": "true" }`)
		case "nullchars":
			contentType = "text/plain; charset=utf-8"
			data = []byte("ab\x00cd\x00\x00")
		case "badutf8":
			w.Header().Set("Bad-Header", "\x80\x81")
			contentType = "text/plain; charset=utf-8"
			data = []byte("ab\x80\x81cd")
		case "binary":
			contentType = "application/octet-stream"
			data = make([]byte, 1000)
			for i := 0; i < 1000; i++ {
				data[i] = byte(i % 255)
			}

			w.Header().Set("Content-Length", "1000")
		}

		w.Header().Set("Date", "Wed, 11 Apr 2018 18:24:30 GMT")
		w.Header().Set("Content-Type", contentType)
		w.WriteHeader(http.StatusOK)
		w.Write(data)
	}))

	if port > 0 {
		// manually create a listener for our test server so that our output is predictable
		l, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
		if err != nil {
			panic(err.Error())
		}
		server.Listener = l
	}
	server.Start()
	return server
}

func TestDoTrace(t *testing.T) {
	defer dates.SetNowSource(dates.DefaultNowSource)

	dates.SetNowSource(dates.NewSequentialNowSource(time.Date(2019, 10, 7, 15, 21, 30, 123456789, time.UTC)))

	server := newTestHTTPServer(52025)

	// test with a text response
	request, err := httpx.NewRequest("GET", server.URL+"?cmd=success", nil, nil)
	require.NoError(t, err)

	trace, err := httpx.DoTrace(http.DefaultClient, request, nil, nil, -1)
	assert.NoError(t, err)
	assert.Equal(t, "GET /?cmd=success HTTP/1.1\r\nHost: 127.0.0.1:52025\r\nUser-Agent: Go-http-client/1.1\r\nAccept-Encoding: gzip\r\n\r\n", string(trace.RequestTrace))
	assert.Equal(t, `{ "ok": "true" }`, string(trace.ResponseBody))
	assert.Equal(t, "HTTP/1.1 200 OK\r\nContent-Length: 16\r\nContent-Type: text/plain; charset=utf-8\r\nDate: Wed, 11 Apr 2018 18:24:30 GMT\r\n\r\n", string(trace.ResponseTrace))
	assert.Equal(t, "{ \"ok\": \"true\" }", string(trace.ResponseBody))
	assert.Equal(t, time.Date(2019, 10, 7, 15, 21, 30, 123456789, time.UTC), trace.StartTime)
	assert.Equal(t, time.Date(2019, 10, 7, 15, 21, 31, 123456789, time.UTC), trace.EndTime)
	assert.Equal(t, 0, trace.Retries)

	assert.Equal(t, "HTTP/1.1 200 OK\r\nContent-Length: 16\r\nContent-Type: text/plain; charset=utf-8\r\nDate: Wed, 11 Apr 2018 18:24:30 GMT\r\n\r\n{ \"ok\": \"true\" }", string(trace.SanitizedResponse("...")))
	assert.Equal(t, ">>>>>>>> GET http://127.0.0.1:52025?cmd=success\nGET /?cmd=success HTTP/1.1\r\nHost: 127.0.0.1:52025\r\nUser-Agent: Go-http-client/1.1\r\nAccept-Encoding: gzip\r\n\r\n\n<<<<<<<<\nHTTP/1.1 200 OK\r\nContent-Length: 16\r\nContent-Type: text/plain; charset=utf-8\r\nDate: Wed, 11 Apr 2018 18:24:30 GMT\r\n\r\n{ \"ok\": \"true\" }", trace.String())

	// test with a binary response
	request, err = httpx.NewRequest("GET", server.URL+"?cmd=binary", nil, nil)
	require.NoError(t, err)

	trace, err = httpx.DoTrace(http.DefaultClient, request, nil, nil, -1)
	assert.NoError(t, err)
	assert.Equal(t, "GET /?cmd=binary HTTP/1.1\r\nHost: 127.0.0.1:52025\r\nUser-Agent: Go-http-client/1.1\r\nAccept-Encoding: gzip\r\n\r\n", string(trace.RequestTrace))
	assert.Equal(t, "HTTP/1.1 200 OK\r\nContent-Length: 1000\r\nContent-Type: application/octet-stream\r\nDate: Wed, 11 Apr 2018 18:24:30 GMT\r\n\r\n", string(trace.ResponseTrace))
	assert.Equal(t, 1000, len(trace.ResponseBody))
	assert.Equal(t, "HTTP/1.1 200 OK\r\nContent-Length: 1000\r\nContent-Type: application/octet-stream\r\nDate: Wed, 11 Apr 2018 18:24:30 GMT\r\n\r\n...", string(trace.SanitizedResponse("...")))

	// test with a response containing null chars
	request, err = httpx.NewRequest("GET", server.URL+"?cmd=nullchars", nil, nil)
	require.NoError(t, err)

	trace, err = httpx.DoTrace(http.DefaultClient, request, nil, nil, -1)
	assert.NoError(t, err)
	assert.Equal(t, "GET /?cmd=nullchars HTTP/1.1\r\nHost: 127.0.0.1:52025\r\nUser-Agent: Go-http-client/1.1\r\nAccept-Encoding: gzip\r\n\r\n", string(trace.RequestTrace))
	assert.Equal(t, "HTTP/1.1 200 OK\r\nContent-Length: 7\r\nContent-Type: text/plain; charset=utf-8\r\nDate: Wed, 11 Apr 2018 18:24:30 GMT\r\n\r\n", string(trace.ResponseTrace))
	assert.Equal(t, 7, len(trace.ResponseBody))
	assert.Equal(t, "HTTP/1.1 200 OK\r\nContent-Length: 7\r\nContent-Type: text/plain; charset=utf-8\r\nDate: Wed, 11 Apr 2018 18:24:30 GMT\r\n\r\nab�cd��", string(trace.SanitizedResponse("...")))

	// test with a response containing invalid UTF8 sequences
	request, err = httpx.NewRequest("GET", server.URL+"?cmd=badutf8", nil, nil)
	require.NoError(t, err)

	trace, err = httpx.DoTrace(http.DefaultClient, request, nil, nil, -1)
	assert.NoError(t, err)
	assert.Equal(t, "GET /?cmd=badutf8 HTTP/1.1\r\nHost: 127.0.0.1:52025\r\nUser-Agent: Go-http-client/1.1\r\nAccept-Encoding: gzip\r\n\r\n", string(trace.RequestTrace))
	assert.Equal(t, "HTTP/1.1 200 OK\r\nContent-Length: 6\r\nBad-Header: \x80\x81\r\nContent-Type: text/plain; charset=utf-8\r\nDate: Wed, 11 Apr 2018 18:24:30 GMT\r\n\r\n", string(trace.ResponseTrace))
	assert.Equal(t, 6, len(trace.ResponseBody))
	assert.Equal(t, "HTTP/1.1 200 OK\r\nContent-Length: 6\r\nBad-Header: �\r\nContent-Type: text/plain; charset=utf-8\r\nDate: Wed, 11 Apr 2018 18:24:30 GMT\r\n\r\n...", string(trace.SanitizedResponse("...")))
}

func TestMaxBodyBytes(t *testing.T) {
	defer httpx.SetRequestor(httpx.DefaultRequestor)

	testBody := `abcdefghijklmnopqrstuvwxyz`

	httpx.SetRequestor(httpx.NewMockRequestor(map[string][]httpx.MockResponse{
		"https://temba.io": {
			httpx.NewMockResponse(200, nil, testBody),
			httpx.NewMockResponse(200, nil, testBody),
			httpx.NewMockResponse(200, nil, testBody),
			httpx.NewMockResponse(200, nil, testBody),
		},
	}))

	call := func(maxBodyBytes int) (*httpx.Trace, error) {
		request, _ := http.NewRequest("GET", "https://temba.io", nil)
		return httpx.DoTrace(http.DefaultClient, request, nil, nil, maxBodyBytes)
	}

	trace, err := call(-1) // no body limit
	assert.NoError(t, err)
	assert.Equal(t, "HTTP/1.0 200 OK\r\nContent-Length: 26\r\n\r\n", string(trace.ResponseTrace))
	assert.Equal(t, testBody, string(trace.ResponseBody))

	trace, err = call(1000) // limit bigger than body
	assert.NoError(t, err)
	assert.Equal(t, "HTTP/1.0 200 OK\r\nContent-Length: 26\r\n\r\n", string(trace.ResponseTrace))
	assert.Equal(t, testBody, string(trace.ResponseBody))

	trace, err = call(len(testBody)) // limit same as body
	assert.NoError(t, err)
	assert.Equal(t, "HTTP/1.0 200 OK\r\nContent-Length: 26\r\n\r\n", string(trace.ResponseTrace))
	assert.Equal(t, testBody, string(trace.ResponseBody))

	trace, err = call(10) // limit smaller than body
	assert.EqualError(t, err, `webhook response body exceeds 10 bytes limit`)
	assert.Equal(t, "HTTP/1.0 200 OK\r\nContent-Length: 26\r\n\r\n", string(trace.ResponseTrace))
	assert.Equal(t, ``, string(trace.ResponseBody))
}

func TestNonUTF8(t *testing.T) {
	defer httpx.SetRequestor(httpx.DefaultRequestor)

	httpx.SetRequestor(httpx.NewMockRequestor(map[string][]httpx.MockResponse{
		"https://temba.io": {
			httpx.MockResponse{Status: 200, Headers: nil, Body: []byte{'\xc3', '\x28'}},
		},
	}))

	request, err := httpx.NewRequest("GET", "https://temba.io", nil, nil)
	require.NoError(t, err)

	trace, err := httpx.DoTrace(http.DefaultClient, request, nil, nil, -1)
	assert.NoError(t, err)
	assert.Equal(t, "GET / HTTP/1.1\r\nHost: temba.io\r\nUser-Agent: Go-http-client/1.1\r\nAccept-Encoding: gzip\r\n\r\n", string(trace.RequestTrace))
	assert.Equal(t, "HTTP/1.0 200 OK\r\nContent-Length: 2\r\n\r\n", string(trace.ResponseTrace))
	assert.Equal(t, []byte{'\xc3', '\x28'}, trace.ResponseBody)
	assert.True(t, utf8.Valid(trace.ResponseTrace))
	assert.False(t, utf8.Valid(trace.ResponseBody))
}
