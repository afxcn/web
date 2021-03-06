package web

import (
	"encoding/json"
	"errors"
	"log"
	"mime"
	"reflect"
	"strconv"
	"strings"
)

func contentType(val string) string {
	var ctype string

	if strings.ContainsRune(val, '/') {
		ctype = val
	} else {
		if !strings.HasPrefix(val, ".") {
			val = "." + val
		}
		ctype = mime.TypeByExtension(val)
	}

	return ctype
}

func logf(format string, v ...interface{}) {
	log.Printf(format, v...)
}

// cleanPath
// wrap path with / like: "user" return "/user/"
func cleanPath(path string) string {

	if len(path) > 0 {
		if path[0] != '/' {
			path = "/" + path
		}
	} else {
		path = "/"
	}

	pos := len(path) - 1

	if pos >= 0 {
		if path[pos] != '/' {
			path = path + "/"
		}
	} else {
		path = "/"
	}

	return path
}

// tryParse try parse val to v
func tryParse(val string, v interface{}) error {
	if v == nil {
		return errors.New("TryParse(nil)")
	}

	rv := reflect.ValueOf(v)

	if rv.Kind() != reflect.Ptr {
		return errors.New("TryParse(non-pointer " + reflect.TypeOf(v).String() + ")")
	}

	if rv.IsNil() {
		return errors.New("TryParse(nil)")
	}

	for rv.Kind() == reflect.Ptr && !rv.IsNil() {
		rv = rv.Elem()
	}

	if !rv.CanSet() {
		return errors.New("TryParse(can not set value to v)")
	}

	switch rv.Interface().(type) {
	case string:
		rv.SetString(val)
		return nil
	case int, int64:
		d, err := strconv.ParseInt(val, 10, 64)
		if err != nil {
			return err
		}
		rv.SetInt(d)
		return nil
	case int32:
		d, err := strconv.ParseInt(val, 10, 32)
		if err != nil {
			return err
		}
		rv.SetInt(d)
		return nil
	default:
		return json.Unmarshal([]byte(val), v)
	}
}
