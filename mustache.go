package web

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path"
	"reflect"
	"strings"
)

type textElement struct {
	text []byte
}

type varElement struct {
	name string
	raw  bool
}

type sectionElement struct {
	name      string
	inverted  bool
	startline int
	elems     []interface{}
}

type Template struct {
	data    string
	otag    string
	ctag    string
	p       int
	curline int
	dir     string
	elems   []interface{}
}

type parseError struct {
	line    int
	message string
}

func (p parseError) Error() string { return fmt.Sprintf("line %d: %s", p.line, p.message) }

var (
	esc_quot = []byte("&quot;")
	esc_apos = []byte("&apos;")
	esc_amp  = []byte("&amp;")
	esc_lt   = []byte("&lt;")
	esc_gt   = []byte("&gt;")
)

func (template *Template) readString(s string) (string, error) {
	i := template.p
	newlines := 0
	for true {
		//are we at the end of the string?
		if i+len(s) > len(template.data) {
			return template.data[template.p:], io.EOF
		}

		if template.data[i] == '\n' {
			newlines++
		}

		if template.data[i] != s[0] {
			i++
			continue
		}

		match := true
		for j := 1; j < len(s); j++ {
			if s[j] != template.data[i+j] {
				match = false
				break
			}
		}

		if match {
			e := i + len(s)
			text := template.data[template.p:e]
			template.p = e

			template.curline += newlines
			return text, nil
		} else {
			i++
		}
	}

	//should never be here
	return "", nil
}

func (template *Template) parsePartial(name string) (*Template, error) {
	filenames := []string{
		path.Join(template.dir, name),
		path.Join(template.dir, name+".mustache"),
		path.Join(template.dir, name+".stache"),
		name,
		name + ".mustache",
		name + ".stache",
	}
	var filename string
	for _, name := range filenames {
		f, err := os.Open(name)
		if err == nil {
			filename = name
			f.Close()
			break
		}
	}
	if filename == "" {
		return nil, errors.New(fmt.Sprintf("Could not find partial %q", name))
	}

	partial, err := ParseFile(filename)

	if err != nil {
		return nil, err
	}

	return partial, nil
}

func (template *Template) parseSection(section *sectionElement) error {
	for {
		text, err := template.readString(template.otag)

		if err == io.EOF {
			return parseError{section.startline, "Section " + section.name + " has no closing tag"}
		}

		// put text into an item
		text = text[0 : len(text)-len(template.otag)]
		section.elems = append(section.elems, &textElement{[]byte(text)})
		if template.p < len(template.data) && template.data[template.p] == '{' {
			text, err = template.readString("}" + template.ctag)
		} else {
			text, err = template.readString(template.ctag)
		}

		if err == io.EOF {
			//put the remaining text in a block
			return parseError{template.curline, "unmatched open tag"}
		}

		//trim the close tag off the text
		tag := strings.TrimSpace(text[0 : len(text)-len(template.ctag)])

		if len(tag) == 0 {
			return parseError{template.curline, "empty tag"}
		}
		switch tag[0] {
		case '!':
			//ignore comment
			break
		case '#', '^':
			name := strings.TrimSpace(tag[1:])

			//ignore the newline when a section starts
			if len(template.data) > template.p && template.data[template.p] == '\n' {
				template.p += 1
			} else if len(template.data) > template.p+1 && template.data[template.p] == '\r' && template.data[template.p+1] == '\n' {
				template.p += 2
			}

			se := sectionElement{name, tag[0] == '^', template.curline, []interface{}{}}
			err := template.parseSection(&se)
			if err != nil {
				return err
			}
			section.elems = append(section.elems, &se)
		case '/':
			name := strings.TrimSpace(tag[1:])
			if name != section.name {
				return parseError{template.curline, "interleaved closing tag: " + name}
			} else {
				return nil
			}
		case '>':
			name := strings.TrimSpace(tag[1:])
			partial, err := template.parsePartial(name)
			if err != nil {
				return err
			}
			section.elems = append(section.elems, partial)
		case '=':
			if tag[len(tag)-1] != '=' {
				return parseError{template.curline, "Invalid meta tag"}
			}
			tag = strings.TrimSpace(tag[1 : len(tag)-1])
			newtags := strings.SplitN(tag, " ", 2)
			if len(newtags) == 2 {
				template.otag = newtags[0]
				template.ctag = newtags[1]
			}
		case '{':
			if tag[len(tag)-1] == '}' {
				//use a raw tag
				section.elems = append(section.elems, &varElement{tag[1 : len(tag)-1], true})
			}
		default:
			section.elems = append(section.elems, &varElement{tag, false})
		}
	}

	return nil
}

func (template *Template) parse() error {
	for {
		text, err := template.readString(template.otag)
		if err == io.EOF {
			//put the remaining text in a block
			template.elems = append(template.elems, &textElement{[]byte(text)})
			return nil
		}

		// put text into an item
		text = text[0 : len(text)-len(template.otag)]
		template.elems = append(template.elems, &textElement{[]byte(text)})

		if template.p < len(template.data) && template.data[template.p] == '{' {
			text, err = template.readString("}" + template.ctag)
		} else {
			text, err = template.readString(template.ctag)
		}

		if err == io.EOF {
			//put the remaining text in a block
			return parseError{template.curline, "unmatched open tag"}
		}

		//trim the close tag off the text
		tag := strings.TrimSpace(text[0 : len(text)-len(template.ctag)])
		if len(tag) == 0 {
			return parseError{template.curline, "empty tag"}
		}
		switch tag[0] {
		case '!':
			//ignore comment
			break
		case '#', '^':
			name := strings.TrimSpace(tag[1:])

			if len(template.data) > template.p && template.data[template.p] == '\n' {
				template.p += 1
			} else if len(template.data) > template.p+1 && template.data[template.p] == '\r' && template.data[template.p+1] == '\n' {
				template.p += 2
			}

			se := sectionElement{name, tag[0] == '^', template.curline, []interface{}{}}
			err := template.parseSection(&se)
			if err != nil {
				return err
			}
			template.elems = append(template.elems, &se)
		case '/':
			return parseError{template.curline, "unmatched close tag"}
		case '>':
			name := strings.TrimSpace(tag[1:])
			partial, err := template.parsePartial(name)
			if err != nil {
				return err
			}
			template.elems = append(template.elems, partial)
		case '=':
			if tag[len(tag)-1] != '=' {
				return parseError{template.curline, "Invalid meta tag"}
			}
			tag = strings.TrimSpace(tag[1 : len(tag)-1])
			newtags := strings.SplitN(tag, " ", 2)
			if len(newtags) == 2 {
				template.otag = newtags[0]
				template.ctag = newtags[1]
			}
		case '{':
			//use a raw tag
			if tag[len(tag)-1] == '}' {
				template.elems = append(template.elems, &varElement{tag[1 : len(tag)-1], true})
			}
		default:
			template.elems = append(template.elems, &varElement{tag, false})
		}
	}

	return nil
}

// See if name is a method of the value at some level of indirection.
// The return values are the result of the call (which may be nil if
// there's trouble) and whether a method of the right name exists with
// any signature.
func callMethod(data reflect.Value, name string) (result reflect.Value, found bool) {
	found = false
	// Method set depends on pointerness, and the value may be arbitrarily
	// indirect.  Simplest approach is to walk down the pointer chain and
	// see if we can find the method at each step.
	// Most steps will see NumMethod() == 0.
	for {
		typ := data.Type()
		if nMethod := data.Type().NumMethod(); nMethod > 0 {
			for i := 0; i < nMethod; i++ {
				method := typ.Method(i)
				if method.Name == name {

					found = true // we found the name regardless
					// does receiver type match? (pointerness might be off)
					if typ == method.Type.In(0) {
						return call(data, method), found
					}
				}
			}
		}
		if nd := data; nd.Kind() == reflect.Ptr {
			data = nd.Elem()
		} else {
			break
		}
	}
	return
}

// Invoke the method. If its signature is wrong, return nil.
func call(v reflect.Value, method reflect.Method) reflect.Value {
	funcType := method.Type
	// Method must take no arguments, meaning as a func it has one argument (the receiver)
	if funcType.NumIn() != 1 {
		return reflect.Value{}
	}
	// Method must return a single value.
	if funcType.NumOut() == 0 {
		return reflect.Value{}
	}
	// Result will be the zeroth element of the returned slice.
	return method.Func.Call([]reflect.Value{v})[0]
}

// Evaluate interfaces and pointers looking for a value that can look up the name, via a
// struct field, method, or map key, and return the result of the lookup.
func lookup(contextChain []interface{}, name string) reflect.Value {
	// dot notation
	if name != "." && strings.Contains(name, ".") {
		parts := strings.SplitN(name, ".", 2)

		v := lookup(contextChain, parts[0])
		return lookup([]interface{}{v}, parts[1])
	}

	defer func() {
		if r := recover(); r != nil {
			fmt.Printf("Panic while looking up %q: %s\n", name, r)
		}
	}()

Outer:
	for _, ctx := range contextChain { //i := len(contextChain) - 1; i >= 0; i-- {
		v := ctx.(reflect.Value)
		for v.IsValid() {
			typ := v.Type()
			if n := v.Type().NumMethod(); n > 0 {
				for i := 0; i < n; i++ {
					m := typ.Method(i)
					mtyp := m.Type
					if m.Name == name && mtyp.NumIn() == 1 {
						return v.Method(i).Call(nil)[0]
					}
				}
			}
			if name == "." {
				return v
			}
			switch av := v; av.Kind() {
			case reflect.Ptr:
				v = av.Elem()
			case reflect.Interface:
				v = av.Elem()
			case reflect.Struct:
				ret := av.FieldByName(name)
				if ret.IsValid() {
					return ret
				} else {
					continue Outer
				}
			case reflect.Map:
				ret := av.MapIndex(reflect.ValueOf(name))
				if ret.IsValid() {
					return ret
				} else {
					continue Outer
				}
			default:
				continue Outer
			}
		}
	}
	return reflect.Value{}
}

func isEmpty(v reflect.Value) bool {
	if !v.IsValid() || v.Interface() == nil {
		return true
	}

	valueInd := indirect(v)
	if !valueInd.IsValid() {
		return true
	}
	switch val := valueInd; val.Kind() {
	case reflect.Bool:
		return !val.Bool()
	case reflect.Slice:
		return val.Len() == 0
	}

	return false
}

func indirect(v reflect.Value) reflect.Value {
loop:
	for v.IsValid() {
		switch av := v; av.Kind() {
		case reflect.Ptr:
			v = av.Elem()
		case reflect.Interface:
			v = av.Elem()
		default:
			break loop
		}
	}
	return v
}

func renderSection(section *sectionElement, contextChain []interface{}, buf io.Writer) {
	value := lookup(contextChain, section.name)
	var context = contextChain[len(contextChain)-1].(reflect.Value)
	var contexts = []interface{}{}
	// if the value is nil, check if it's an inverted section
	isEmpty := isEmpty(value)
	if isEmpty && !section.inverted || !isEmpty && section.inverted {
		return
	} else if !section.inverted {
		valueInd := indirect(value)
		switch val := valueInd; val.Kind() {
		case reflect.Slice:
			for i := 0; i < val.Len(); i++ {
				contexts = append(contexts, val.Index(i))
			}
		case reflect.Array:
			for i := 0; i < val.Len(); i++ {
				contexts = append(contexts, val.Index(i))
			}
		case reflect.Map, reflect.Struct:
			contexts = append(contexts, value)
		default:
			contexts = append(contexts, context)
		}
	} else if section.inverted {
		contexts = append(contexts, context)
	}

	chain2 := make([]interface{}, len(contextChain)+1)
	copy(chain2[1:], contextChain)
	//by default we execute the section
	for _, ctx := range contexts {
		chain2[0] = ctx
		for _, elem := range section.elems {
			renderElement(elem, chain2, buf)
		}
	}
}

func renderElement(element interface{}, contextChain []interface{}, buf io.Writer) {
	switch elem := element.(type) {
	case *textElement:
		buf.Write(elem.text)
	case *varElement:
		defer func() {
			if r := recover(); r != nil {
				fmt.Printf("Panic while looking up %q: %s\n", elem.name, r)
			}
		}()
		val := lookup(contextChain, elem.name)

		if val.IsValid() {
			if elem.raw {
				fmt.Fprint(buf, val.Interface())
			} else {
				s := fmt.Sprint(val.Interface())
				htmlEscape(buf, []byte(s))
			}
		}
	case *sectionElement:
		renderSection(elem, contextChain, buf)
	case *Template:
		elem.renderTemplate(contextChain, buf)
	}
}

func (template *Template) renderTemplate(contextChain []interface{}, buf io.Writer) {
	for _, elem := range template.elems {
		renderElement(elem, contextChain, buf)
	}
}

func (template *Template) Render(context ...interface{}) string {
	var buf bytes.Buffer
	var contextChain []interface{}
	for _, c := range context {
		val := reflect.ValueOf(c)
		contextChain = append(contextChain, val)
	}
	template.renderTemplate(contextChain, &buf)
	return buf.String()
}

func (template *Template) RenderInLayout(layout *Template, context ...interface{}) string {
	content := template.Render(context...)
	allContext := make([]interface{}, len(context)+1)
	copy(allContext[1:], context)
	allContext[0] = map[string]string{"content": content}
	return layout.Render(allContext...)
}

func ParseString(data string) (*Template, error) {
	cwd := os.Getenv("CWD")
	template := Template{data, "{{", "}}", 0, 1, cwd, []interface{}{}}
	err := template.parse()

	if err != nil {
		return nil, err
	}

	return &template, err
}

func ParseFile(filename string) (*Template, error) {
	data, err := ioutil.ReadFile(filename)
	if err != nil {
		return nil, err
	}

	dirname, _ := path.Split(filename)

	template := Template{string(data), "{{", "}}", 0, 1, dirname, []interface{}{}}
	err = template.parse()

	if err != nil {
		return nil, err
	}

	return &template, nil
}

func Render(data string, context ...interface{}) string {
	template, err := ParseString(data)
	if err != nil {
		return err.Error()
	}
	return template.Render(context...)
}

func RenderInLayout(data string, layoutData string, context ...interface{}) string {
	layout, err := ParseString(layoutData)
	if err != nil {
		return err.Error()
	}
	template, err := ParseString(data)
	if err != nil {
		return err.Error()
	}
	return template.RenderInLayout(layout, context...)
}

func RenderFile(filename string, context ...interface{}) string {
	template, err := ParseFile(filename)
	if err != nil {
		return err.Error()
	}
	return template.Render(context...)
}

func RenderFileInLayout(filename string, layoutFile string, context ...interface{}) string {
	layout, err := ParseFile(layoutFile)
	if err != nil {
		return err.Error()
	}

	template, err := ParseFile(filename)
	if err != nil {
		return err.Error()
	}
	return template.RenderInLayout(layout, context...)
}