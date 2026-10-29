package filestore

import "net/http"

// SignForTest signs a request as the store would, for the signature's
// tests.
func SignForTest(s *S3, req *http.Request, payloadHash string) {
	s.sign(req, payloadHash, s.opts.Now().UTC())
}
