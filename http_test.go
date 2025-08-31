package rfc9110_7_5

import (
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
)

func TestStreamingRequestBody(t *testing.T) {
	var seqNum atomic.Int32

	var firstServerReadAt int32
	svr := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.ProtoMajor == 1 && r.ProtoMinor == 1 {
			rc := http.NewResponseController(w)
			rc.EnableFullDuplex()
			// EnableFullDuplex is critical to allow the server to begin streaming the response
			// while continuing to read the request body. Otherwise, the server will receive
			// http.ErrBodyReadAfterClose ("http: invalid Read on closed Body") upon reading
			// the request body after it begins writing the response. This is default behavior
			// for HTTP/2, but for HTTP/1.1 the server must explicitly enable it.
		} else {
			t.Fatalf("seq %d server expected HTTP/1.1 request, got HTTP/%d.%d",
				seqNum.Add(1), r.ProtoMajor, r.ProtoMinor)
		}

		rDebug, err := httputil.DumpRequest(r, false)
		if err != nil {
			t.Fatalf("sed %d failed to dump request: %v", seqNum.Add(1), err)
		}
		t.Logf("seq %d server received request:\n%s", seqNum.Add(1), string(rDebug))
		w.WriteHeader(http.StatusOK)
		buf := make([]byte, 1000)
		for {
			n, err := r.Body.Read(buf)
			if err != nil {
				if errors.Is(err, io.EOF) {
					t.Logf("seq %d server read EOF", seqNum.Add(1))
				} else {
					t.Fatalf("seq %d server read error: %v", seqNum.Add(1), err)
				}
				break
			}
			readAt := seqNum.Add(1)
			if firstServerReadAt == 0 {
				firstServerReadAt = readAt
			}
			t.Logf("seq %d server read %d bytes", readAt, n)
			text := string(buf[:n])
			n, err = io.WriteString(w, strings.ToUpper(text))
			if err != nil {
				t.Fatalf("seq %d server write error: %v", seqNum.Add(1), err)
				break
			}
			t.Logf("seq %d server wrote %d bytes", seqNum.Add(1), n)
		}
	}))
	svr.EnableHTTP2 = true
	svr.StartTLS()
	defer svr.Close()

	reqBodyReader, reqBodyWriter := io.Pipe()

	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, svr.URL, reqBodyReader)
	if err != nil {
		t.Fatalf("seq %d failed to create request: %v", seqNum.Add(1), err)
	}

	var lastClientWriteAt int32
	bodyWriteErr := make(chan error)
	go func(ch chan<- error) {
		defer reqBodyWriter.Close()
		for i := 0; i < 10; i++ {
			t.Logf("seq %d client loop iteration %d", seqNum.Add(1), i)
			n, err := reqBodyWriter.Write([]byte(strings.Repeat("helloworld", 100)))
			if err != nil {
				ch <- fmt.Errorf("seq %d client write error: %w", seqNum.Add(1), err)
				break
			}
			lastClientWriteAt = seqNum.Add(1)
			t.Logf("seq %d client wrote %d bytes", lastClientWriteAt, n)
			runtime.Gosched()
		}
		close(bodyWriteErr)
	}(bodyWriteErr)

	proto := &http.Protocols{}
	// proto.SetHTTP2(true)
	client := http.Client{
		Transport: &http.Transport{
			Protocols: proto,
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true,
			},
		},
	}
	resp, err := client.Do(req)
	clientDoAt := seqNum.Add(1)
	t.Logf("seq %d client.Do() returns", clientDoAt)
	if err != nil {
		t.Fatalf("seq %d failed to send request: %v", seqNum.Add(1), err)
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("seq %d unexpected status code: %d", seqNum.Add(1), resp.StatusCode)
	}

	var firstClientReadAt int32
	buf := make([]byte, 1000)
	for {
		n, err := resp.Body.Read(buf)
		if err != nil {
			if errors.Is(err, io.EOF) {
				t.Logf("seq %d client read EOF", seqNum.Add(1))
			} else {
				t.Fatalf("seq %d client read error: %v", seqNum.Add(1), err)
			}
			break
		}
		readAt := seqNum.Add(1)
		if firstClientReadAt == 0 {
			firstClientReadAt = readAt
		}
		t.Logf("seq %d client read %d bytes", readAt, n)
	}

	if err := <-bodyWriteErr; err != nil {
		t.Fatal(err)
	}

	if lastClientWriteAt < clientDoAt {
		t.Fatalf("client finished writing request body before client.Do() returned")
	}
	if lastClientWriteAt < firstServerReadAt {
		t.Fatalf("client finished writing request body before server read any")
	}
	if lastClientWriteAt < firstClientReadAt {
		t.Fatalf("client finished writing request body before client read any response body")
	}
}
