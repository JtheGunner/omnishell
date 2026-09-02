package module_test

import (
	"reflect"
	"testing"

	"github.com/JtheGunner/omnishell/internal/module"
)

func schema() map[string]module.OptionSchema {
	return map[string]module.OptionSchema{
		"ctrl_r":       {Type: "bool", Default: true},
		"default_opts": {Type: "string", Default: "--reverse"},
		"size":         {Type: "int", Default: int64(50000)},
		"replace":      {Type: "list<enum>", Values: []string{"ls", "cat", "find"}, Default: []any{}},
	}
}

func TestValidateOptionsFillsDefaults(t *testing.T) {
	got, err := module.ValidateOptions(schema(), map[string]any{"ctrl_r": false})
	if err != nil {
		t.Fatalf("ValidateOptions: %v", err)
	}
	want := map[string]any{
		"ctrl_r":       false,
		"default_opts": "--reverse",
		"size":         int64(50000),
		"replace":      []string{},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v\nwant %#v", got, want)
	}
}

func TestValidateOptionsCoercesTomlTypes(t *testing.T) {
	got, err := module.ValidateOptions(schema(), map[string]any{
		"size":    int64(10),
		"replace": []any{"ls", "cat"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got["size"] != int64(10) {
		t.Fatalf("size = %#v", got["size"])
	}
	if !reflect.DeepEqual(got["replace"], []string{"ls", "cat"}) {
		t.Fatalf("replace = %#v", got["replace"])
	}
}

func TestValidateOptionsRejectsUnknownKey(t *testing.T) {
	_, err := module.ValidateOptions(schema(), map[string]any{"bogus": 1})
	if err == nil {
		t.Fatal("want error for unknown key")
	}
}

func TestValidateOptionsRejectsEnumNotInValues(t *testing.T) {
	_, err := module.ValidateOptions(schema(), map[string]any{"replace": []any{"grep"}})
	if err == nil {
		t.Fatal("want error for value not in enum")
	}
}

func TestParseValue(t *testing.T) {
	s := schema()
	if v, _ := s["ctrl_r"].ParseValue("false"); v != false {
		t.Fatalf("bool parse = %#v", v)
	}
	if v, _ := s["size"].ParseValue("42"); v != int64(42) {
		t.Fatalf("int parse = %#v", v)
	}
	v, err := s["replace"].ParseValue("ls, find")
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := v.([]string); len(got) != 2 || got[0] != "ls" || got[1] != "find" {
		t.Fatalf("list parse = %#v", v)
	}
	if _, err := s["replace"].ParseValue("ls, grep"); err == nil {
		t.Fatal("want error for grep not in values")
	}
}

func TestOptionsHashDeterministicAndOrderIndependent(t *testing.T) {
	a, _ := module.ValidateOptions(schema(), map[string]any{"ctrl_r": true, "size": int64(1)})
	b, _ := module.ValidateOptions(schema(), map[string]any{"size": int64(1), "ctrl_r": true})
	if module.OptionsHash(a) != module.OptionsHash(b) {
		t.Fatal("hash depends on input order")
	}
	c, _ := module.ValidateOptions(schema(), map[string]any{"ctrl_r": false})
	if module.OptionsHash(a) == module.OptionsHash(c) {
		t.Fatal("different options hashed equal")
	}
}
