package config

/*
GET /auth?params HTTP/1.1
Host: api.example.com
User-Agent: Go-http-client/1.1
Accept: application/json
Accept-Encoding: gzip
*/
type HTTPRequest struct {
	Method  string            // GET, POST
	Path    string            // /auth, /users, /
	Version string            // HTTP/1.1
	Headers map[string]string // Authorization: Bearer XXXXXXXX(key: value), Accept: application/json
	Body    string            // json
}
