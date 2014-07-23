package mustache

import (
	"bytes"
	"fmt"
	"io"
	"reflect"
	"strings"
)

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

// taken from pkg/template
func htmlEscape(w io.Writer, s []byte) {
	var esc []byte
	last := 0
	for i, c := range s {
		switch c {
		case '"':
			esc = esc_quot
		case '\'':
			esc = esc_apos
		case '&':
			esc = esc_amp
		case '<':
			esc = esc_lt
		case '>':
			esc = esc_gt
		default:
			continue
		}
		w.Write(s[last:i])
		w.Write(esc)
		last = i + 1
	}
	w.Write(s[last:])
}

func (tmpl *Template) readString(s string) (string, error) {
	i := tmpl.p
	newlines := 0
	for true {
		//are we at the end of the string?
		if i+len(s) > len(tmpl.data) {
			return tmpl.data[tmpl.p:], io.EOF
		}

		if tmpl.data[i] == '\n' {
			newlines++
		}

		if tmpl.data[i] != s[0] {
			i++
			continue
		}

		match := true
		for j := 1; j < len(s); j++ {
			if s[j] != tmpl.data[i+j] {
				match = false
				break
			}
		}

		if match {
			e := i + len(s)
			text := tmpl.data[tmpl.p:e]
			tmpl.p = e

			tmpl.curline += newlines
			return text, nil
		} else {
			i++
		}
	}

	//should never be here
	return "", nil
}

func (tmpl *Template) parseSection(section *sectionElement) error {
	for {
		text, err := tmpl.readString(tmpl.otag)

		if err == io.EOF {
			return parseError{section.startline, "Section " + section.name + " has no closing tag"}
		}

		// put text into an item
		text = text[0 : len(text)-len(tmpl.otag)]
		section.elems = append(section.elems, text)
		if tmpl.p < len(tmpl.data) && tmpl.data[tmpl.p] == '{' {
			text, err = tmpl.readString("}" + tmpl.ctag)
		} else {
			text, err = tmpl.readString(tmpl.ctag)
		}

		if err == io.EOF {
			//put the remaining text in a block
			return parseError{tmpl.curline, "unmatched open tag"}
		}

		//trim the close tag off the text
		tag := strings.TrimSpace(text[0 : len(text)-len(tmpl.ctag)])

		if len(tag) == 0 {
			return parseError{tmpl.curline, "empty tag"}
		}
		switch tag[0] {
		case '!':
			//ignore comment
			break
		case '#', '^':
			name := strings.TrimSpace(tag[1:])

			//ignore the newline when a section starts
			if len(tmpl.data) > tmpl.p && tmpl.data[tmpl.p] == '\n' {
				tmpl.p += 1
			} else if len(tmpl.data) > tmpl.p+1 && tmpl.data[tmpl.p] == '\r' && tmpl.data[tmpl.p+1] == '\n' {
				tmpl.p += 2
			}

			se := sectionElement{name, tag[0] == '^', tmpl.curline, []interface{}{}}
			err := tmpl.parseSection(&se)
			if err != nil {
				return err
			}
			section.elems = append(section.elems, &se)
		case '/':
			name := strings.TrimSpace(tag[1:])
			if name != section.name {
				return parseError{tmpl.curline, "interleaved closing tag: " + name}
			} else {
				return nil
			}
		case '{':
			if tag[len(tag)-1] == '}' {
				//use a raw tag
				name := strings.TrimSpace(tag[1 : len(tag)-1])
				section.elems = append(section.elems, &varElement{name, true})
			}
		case '&':
			name := strings.TrimSpace(tag[1:len(tag)])
			section.elems = append(section.elems, &varElement{name, true})
		default:
			section.elems = append(section.elems, &varElement{tag, false})
		}
	}

	return nil
}

func (tmpl *Template) parse() error {
	for {
		text, err := tmpl.readString(tmpl.otag)
		if err == io.EOF {
			//put the remaining text in a block
			tmpl.elems = append(tmpl.elems, text)
			return nil
		}

		// put text into an item
		text = text[0 : len(text)-len(tmpl.otag)]
		tmpl.elems = append(tmpl.elems, text)

		if tmpl.p < len(tmpl.data) && tmpl.data[tmpl.p] == '{' {
			text, err = tmpl.readString("}" + tmpl.ctag)
		} else {
			text, err = tmpl.readString(tmpl.ctag)
		}

		if err == io.EOF {
			//put the remaining text in a block
			return parseError{tmpl.curline, "unmatched open tag"}
		}

		//trim the close tag off the text
		tag := strings.TrimSpace(text[0 : len(text)-len(tmpl.ctag)])
		if len(tag) == 0 {
			return parseError{tmpl.curline, "empty tag"}
		}
		switch tag[0] {
		case '!':
			//ignore comment
			break
		case '#', '^':
			name := strings.TrimSpace(tag[1:])

			if len(tmpl.data) > tmpl.p && tmpl.data[tmpl.p] == '\n' {
				tmpl.p += 1
			} else if len(tmpl.data) > tmpl.p+1 && tmpl.data[tmpl.p] == '\r' && tmpl.data[tmpl.p+1] == '\n' {
				tmpl.p += 2
			}

			se := sectionElement{name, tag[0] == '^', tmpl.curline, []interface{}{}}
			err := tmpl.parseSection(&se)
			if err != nil {
				return err
			}
			tmpl.elems = append(tmpl.elems, &se)
		case '/':
			return parseError{tmpl.curline, "unmatched close tag"}
		case '{':
			//use a raw tag
			if tag[len(tag)-1] == '}' {
				name := strings.TrimSpace(tag[1 : len(tag)-1])
				tmpl.elems = append(tmpl.elems, &varElement{name, true})
			}
		case '&':
			name := strings.TrimSpace(tag[1:len(tag)])
			tmpl.elems = append(tmpl.elems, &varElement{name, true})
		default:
			tmpl.elems = append(tmpl.elems, &varElement{tag, false})
		}
	}

	return nil
}

// Evaluate interfaces and pointers looking for a value that can look up the name, via a
// struct field, method, or map key, and return the result of the lookup.
func lookup(contextChain []reflect.Value, name string) reflect.Value {
	// dot notation
	if name != "." && strings.Contains(name, ".") {
		parts := strings.SplitN(name, ".", 2)
		v := lookup(contextChain, parts[0])
		return lookup([]reflect.Value{v}, parts[1])
	}

	defer func() {
		if r := recover(); r != nil {
			fmt.Printf("Panic while looking up %q: %s\n", name, r)
		}
	}()

Outer:
	for _, ctx := range contextChain {
		ctx = reflect.Indirect(ctx)
		for ctx.IsValid() {
			if name == "." {
				return ctx
			}
			switch ctx.Kind() {
			case reflect.Struct:
				v := ctx.FieldByName(name)
				if !v.IsValid() {
					continue Outer
				}
				return v
			case reflect.Map:
				v := ctx.MapIndex(reflect.ValueOf(name))
				if !v.IsValid() {
					continue Outer
				}
				return v
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

func renderSection(section *sectionElement, contextChain []reflect.Value, buf io.Writer) {
	value := lookup(contextChain, section.name)
	var context reflect.Value
	var contexts = []reflect.Value{}

	// guard against empty contextChain
	if len(contextChain) > 0 {
		context = contextChain[len(contextChain)-1]
	}

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

	chain2 := make([]reflect.Value, len(contextChain)+1)
	copy(chain2[1:], contextChain)
	//by default we execute the section
	for _, ctx := range contexts {
		chain2[0] = ctx
		for _, elem := range section.elems {
			renderElement(elem, chain2, buf)
		}
	}
}

func renderElement(element interface{}, contextChain []reflect.Value, buf io.Writer) {
	switch elem := element.(type) {
	case string:
		io.WriteString(buf, elem)
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

func (tmpl *Template) renderTemplate(contextChain []reflect.Value, buf io.Writer) {
	for _, elem := range tmpl.elems {
		renderElement(elem, contextChain, buf)
	}
}

func (tmpl *Template) Render(context ...interface{}) string {
	var buf bytes.Buffer
	var contextChain []reflect.Value
	for _, c := range context {
		val := reflect.ValueOf(c)
		contextChain = append(contextChain, val)
	}
	tmpl.renderTemplate(contextChain, &buf)
	return buf.String()
}

func ParseString(data string) (*Template, error) {
	tmpl := Template{data, "{{", "}}", 0, 1, []interface{}{}}
	err := tmpl.parse()

	if err != nil {
		return nil, err
	}

	return &tmpl, err
}

func Render(data string, context ...interface{}) string {
	tmpl, err := ParseString(data)
	if err != nil {
		return err.Error()
	}
	return tmpl.Render(context...)
}
