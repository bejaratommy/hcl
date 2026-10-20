// Copyright IBM Corp. 2014, 2025
// SPDX-License-Identifier: MPL-2.0

package gohcl

import (
	"reflect"
	"strings"
	"testing"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/hashicorp/hcl/v2/hclwrite"
)

type EmbedA struct {
	A string `hcl:"a"`
}

type EmbedB struct {
	EmbedA
	B int `hcl:"b"`
}

type EmbedPtrB struct {
	*EmbedA
	B int `hcl:"b"`
}

type embedFile struct {
	Stuff []EmbedB `hcl:"stuff,block"`
}

type embedPtrFile struct {
	Stuff EmbedPtrB `hcl:"stuff,block"`
}

type embedTagged struct {
	EmbedA `hcl:"nested,block"`
	B      int `hcl:"b"`
}

func TestDecodeEmbeddedStruct(t *testing.T) {
	src := []byte("stuff {\n  a = \"asdf\"\n  b = 123\n}\n")
	file, diags := hclsyntax.ParseConfig(src, "test.hcl", hcl.InitialPos)
	if diags.HasErrors() {
		t.Fatal(diags.Error())
	}

	var got embedFile
	diags = DecodeBody(file.Body, nil, &got)
	if diags.HasErrors() {
		t.Fatal(diags.Error())
	}
	want := embedFile{Stuff: []EmbedB{{EmbedA: EmbedA{A: "asdf"}, B: 123}}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

func TestDecodeEmbeddedPointerStruct(t *testing.T) {
	src := []byte("stuff {\n  a = \"asdf\"\n  b = 123\n}\n")
	file, diags := hclsyntax.ParseConfig(src, "test.hcl", hcl.InitialPos)
	if diags.HasErrors() {
		t.Fatal(diags.Error())
	}

	var got embedPtrFile
	diags = DecodeBody(file.Body, nil, &got)
	if diags.HasErrors() {
		t.Fatal(diags.Error())
	}
	if got.Stuff.EmbedA == nil || got.Stuff.A != "asdf" || got.Stuff.B != 123 {
		t.Fatalf("got %#v", got.Stuff)
	}
}

func TestEmbeddedWithTagIsNotPromoted(t *testing.T) {
	schema, partial := ImpliedBodySchema(embedTagged{})
	if partial {
		t.Fatal("unexpected partial schema")
	}
	if len(schema.Attributes) != 1 || schema.Attributes[0].Name != "b" {
		t.Fatalf("attributes: %#v", schema.Attributes)
	}
	if len(schema.Blocks) != 1 || schema.Blocks[0].Type != "nested" {
		t.Fatalf("blocks: %#v", schema.Blocks)
	}
}

func TestEncodeEmbeddedStruct(t *testing.T) {
	val := EmbedB{EmbedA: EmbedA{A: "asdf"}, B: 123}
	f := hclwrite.NewEmptyFile()
	EncodeIntoBody(&val, f.Body())
	got := string(f.Bytes())
	if !strings.Contains(got, `a = "asdf"`) || !strings.Contains(got, `b = 123`) {
		t.Fatalf("encoded %q", got)
	}
}

func TestEncodeNilEmbeddedPointerOmitsFields(t *testing.T) {
	val := EmbedPtrB{B: 123}
	f := hclwrite.NewEmptyFile()
	EncodeIntoBody(&val, f.Body())
	got := string(f.Bytes())
	if strings.Contains(got, "a =") {
		t.Fatalf("nil embedded pointer was encoded: %q", got)
	}
	if !strings.Contains(got, "b = 123") {
		t.Fatalf("encoded %q", got)
	}
}
