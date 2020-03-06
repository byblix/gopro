package server

import (
	"encoding/json"
	"net/http"

	"errors"
)

type simpleResponse struct {
	Msg  string `json:"msg,omitempty"`
	Code int    `json:"code,omitempty"`
}

type HttpStatusCode int

var ErrPanicRecover = errors.New("")
var ErrJSONEncoding = errors.New("json marshall encoding to byte array")
var ErrJSONDecoding = errors.New("json unmarshall decoding")
var ErrBadTokenHeader = errors.New("no or wrong token found in header")
var ErrBadDateRequest = errors.New("bad values or wrong date time")

const (
	_ = iota + 519
	StatusPanic
	StatusJSONEncode // Marshall
	StatusJSONDecode // Unmarshall
	StatusBadTokenHeader
	StatusBadDateTime
)

var StatusText = map[HttpStatusCode]error{
	StatusJSONEncode:     ErrJSONEncoding,
	StatusJSONDecode:     ErrJSONDecoding,
	StatusBadTokenHeader: ErrBadTokenHeader,
	StatusBadDateTime:    ErrBadDateRequest,
}

// writes client or returns json encoding error
func WriteClient(w http.ResponseWriter, code HttpStatusCode) (jsonerr HttpStatusCode) {
	enc := json.NewEncoder(w)
	msg, ok := code.StatusText()
	if !ok {
		if err := enc.Encode(&simpleResponse{
			Code: http.StatusInternalServerError,
			Msg:  "statuswriter failed output",
		}); err != nil {
			log.Error(err)
		}
		return
	}
	w.WriteHeader(int(code))
	res := &simpleResponse{
		Code: int(code),
		Msg:  msg,
	}
	if err := enc.Encode(res); err != nil {
		return StatusJSONEncode
	}
	return 0
}

// Output log if necessary
func (code HttpStatusCode) StatusText() (string, bool) {
	if http.StatusText(int(code)) != "" {
		log.Warnf("http stdlib statustext code: %v", code)
		return http.StatusText(int(code)), true
	} else {
		if val, ok := StatusText[code]; ok {
			return val.Error(), ok
		} else {
			log.Infof("code %v - possibly nil pointer err", int(code))
			return "unknown error occurred internally - contact Simon on Slack.", false
		}
	}
}

func (code HttpStatusCode) LogError(err error) {
	log.Error(err)
	msg, ok := code.StatusText()
	log.Errorf("err val: %+v \n ClientMsg: %s (statuscode anticipated and defined in API?: %v)", err, msg, ok)
}
