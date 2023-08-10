// Copyright (c) 2023 Canonical Ltd
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License version 3 as
// published by the Free Software Foundation.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License
// along with this program.  If not, see <http://www.gnu.org/licenses/>.

package daemon

import (
	"bytes"
	"net/http"
	"net/http/httptest"

	. "gopkg.in/check.v1"
)

	req.Header.Set(key, value)
        headers := http.Header{
                "Accept": []string{"multipart/form-data"},
        }

func doRequest(c *C, f ResponseFunc, method, url string, query url.Values, headers http.Header, body []byte) (*http.Response, *bytes.Buffer) {
        var bodyReader io.Reader
        if body != nil {
                bodyReader = bytes.NewBuffer(body)
        }
        req, err := http.NewRequest(method, url, bodyReader)
        c.Assert(err, IsNil)
        if query != nil {
                req.URL.RawQuery = query.Encode()
        }
        req.Header = headers
        handler := f(apiCmd(url), req, nil)
        recorder := httptest.NewRecorder()
        handler.ServeHTTP(recorder, req)
        response := recorder.Result()
        return response, recorder.Body
}


func (s *apiSuite) TestDevicePostErrors(c *C) {
	var tests = []struct {
		payload string
		status  int
		message string
	}{
		{"@", 400, `cannot decode request body: invalid character '@' looking for beginning of value`},
		{`{"action": "poweroff"}`, 400, `invalid action "poweroff"`},
	}

	_ = s.daemon(c)
	deviceCmd := apiCmd("/v1/device")

	for _, test := range tests {
		req, err := http.NewRequest("POST", "/v1/device", bytes.NewBufferString(test.payload))
		c.Assert(err, IsNil)
		rsp := v1PostDevice(deviceCmd, req, nil).(*resp)
		rec := httptest.NewRecorder()
		rsp.ServeHTTP(rec, req)
		c.Assert(rec.Code, Equals, test.status)
		c.Assert(rsp.Status, Equals, test.status)
		c.Assert(rsp.Type, Equals, ResponseTypeError)
		c.Assert(rsp.Result.(*errorResult).Message, Matches, test.message)
	}
}
//
//func (s *apiSuite) TestLayersAddAppend(c *C) {
//	writeTestLayer(s.pebbleDir, planLayer)
//	_ = s.daemon(c)
//	layersCmd := apiCmd("/v1/layers")
//
//	payload := `{"action": "add", "label": "foo", "format": "yaml", "layer": "services:\n dynamic:\n  override: replace\n  command: echo dynamic\n"}`
//	req, err := http.NewRequest("POST", "/v1/layers", bytes.NewBufferString(payload))
//	c.Assert(err, IsNil)
//	rsp := v1PostLayers(layersCmd, req, nil).(*resp)
//	rec := httptest.NewRecorder()
//	rsp.ServeHTTP(rec, req)
//	c.Assert(rec.Code, Equals, 200)
//	c.Assert(rsp.Status, Equals, 200)
//	c.Assert(rsp.Type, Equals, ResponseTypeSync)
//	c.Assert(rsp.Result.(bool), Equals, true)
//	c.Assert(s.planYAML(c), Equals, `
//services:
//    dynamic:
//        override: replace
//        command: echo dynamic
//    static:
//        override: replace
//        command: echo static
//`[1:])
//	s.planLayersHasLen(c, 2)
//}
//
//func (s *apiSuite) TestLayersAddCombine(c *C) {
//	writeTestLayer(s.pebbleDir, planLayer)
//	_ = s.daemon(c)
//	layersCmd := apiCmd("/v1/layers")
//
//	payload := `{"action": "add", "combine": true, "label": "base", "format": "yaml", "layer": "services:\n dynamic:\n  override: replace\n  command: echo dynamic\n"}`
//	req, err := http.NewRequest("POST", "/v1/layers", bytes.NewBufferString(payload))
//	c.Assert(err, IsNil)
//	rsp := v1PostLayers(layersCmd, req, nil).(*resp)
//	rec := httptest.NewRecorder()
//	rsp.ServeHTTP(rec, req)
//	c.Assert(rec.Code, Equals, 200)
//	c.Assert(rsp.Status, Equals, 200)
//	c.Assert(rsp.Type, Equals, ResponseTypeSync)
//	c.Assert(rsp.Result.(bool), Equals, true)
//	c.Assert(s.planYAML(c), Equals, `
//services:
//    dynamic:
//        override: replace
//        command: echo dynamic
//    static:
//        override: replace
//        command: echo static
//`[1:])
//	s.planLayersHasLen(c, 1)
//}
//
//func (s *apiSuite) TestLayersCombineFormatError(c *C) {
//	writeTestLayer(s.pebbleDir, planLayer)
//	_ = s.daemon(c)
//	layersCmd := apiCmd("/v1/layers")
//
//	payload := `{"action": "add", "combine": true, "label": "base", "format": "yaml", "layer": "services:\n dynamic:\n  command: echo dynamic\n"}`
//	req, err := http.NewRequest("POST", "/v1/layers", bytes.NewBufferString(payload))
//	c.Assert(err, IsNil)
//	rsp := v1PostLayers(layersCmd, req, nil).(*resp)
//	rec := httptest.NewRecorder()
//	rsp.ServeHTTP(rec, req)
//	c.Assert(rec.Code, Equals, http.StatusBadRequest)
//	c.Assert(rsp.Status, Equals, http.StatusBadRequest)
//	c.Assert(rsp.Type, Equals, ResponseTypeError)
//	result := rsp.Result.(*errorResult)
//	c.Assert(result.Message, Matches, `layer "base" must define "override" for service "dynamic"`)
//}
