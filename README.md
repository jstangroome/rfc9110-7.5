# Golang compatibility RFC 9110 HTTP Semantics with §7.5 Response Correlation

In [RFC 9110](https://www.rfc-editor.org/rfc/rfc9110.html), §7.5 states:
>All responses, regardless of the status code (including interim responses) can be sent at any time after a request is received, even if the request is not yet complete. A response can complete before its corresponding request is complete

I.e. a Golang HTTP server should be able to begin sending the response body while it continues
to consume the request body. Likewise, a Golang HTTP client should be able to begin receiving
the response body while it continues to send the request body.

It appears this only became possible with the Golang net/http package for HTTP/1.1 requests from
[Golang v1.21](https://go.dev/doc/go1.21#nethttppkgnethttp) onward with the introduction of the
[net/http.ResponseController.EnableFullDuplex()](https://pkg.go.dev/net/http#ResponseController.EnableFullDuplex)
method.

If the Golang server's http.Handler does not call `ResponseController.EnableFullDuplex()` on
HTTP/1.1 requests, the handler will receive
[http.ErrBodyReadAfterClose](https://pkg.go.dev/net/http#ErrBodyReadAfterClose)
("http: invalid Read on closed Body") upon reading the request body after has started writing the 
response.
