package javascript

import (
	"reflect"
	"sort"
	"testing"

	"github.com/rburns629/urfluency/internal/analysis"
)

func TestSecurityRules(t *testing.T) {
	src := "const { exec } = require('child_process');\n" +
		"exec(`ls ${dir}`);\n" +
		"exec('ls');\n" +
		"db.query(`SELECT * FROM t WHERE id = ${id}`);\n" +
		"db.query(sql`SELECT * FROM t WHERE id = ${id}`);\n" +
		"db.query('SELECT 1', [id]);\n" +
		"el.innerHTML = html;\n" +
		"el.innerHTML = '<b>static</b>';\n" +
		"eval(code);\n" +
		"new Function(code);\n" +
		"setTimeout('tick()', 10);\n" +
		"const agent = { rejectUnauthorized: false };\n" +
		"process.env.NODE_TLS_REJECT_UNAUTHORIZED = '0';\n" +
		"/x/.exec(input);\n"
	want := []string{
		"js.child-process-injection:2", "js.eval:10", "js.eval:11", "js.eval:9", "js.html-injection:7",
		"js.sql-string-built:4", "js.tls-verify-disabled:12", "js.tls-verify-disabled:13",
	}
	if got := findings(t, JavaScript{}, "src/a.js", src); !reflect.DeepEqual(got, want) {
		t.Errorf("got %v\nwant %v", got, want)
	}
	noChildProcess := "exec(`ls ${dir}`);\n"
	if got := findings(t, JavaScript{}, "src/b.js", noChildProcess); len(got) != 0 {
		t.Errorf("exec without child_process import should not match: %v", got)
	}
}

func TestReactRules(t *testing.T) {
	src := `import React, { useState } from 'react';
export function List({ items, show }) {
  if (show) { useState(1); }
  const [x] = useState(0);
  return (
    <ul>
      {items.map((it, i) => <li key={i}>{it}</li>)}
      {items.map((it) => <li>{it}</li>)}
      {items.map((it) => <li key={it.id}>{it}</li>)}
      <div dangerouslySetInnerHTML={{ __html: x }} />
    </ul>
  );
}
`
	want := []string{"react.dangerously-set-inner-html:10", "react.hook-in-condition:3", "react.index-as-key:7", "react.missing-key:8"}
	if got := findings(t, JavaScript{}, "src/List.jsx", src); !reflect.DeepEqual(got, want) {
		t.Errorf("got %v\nwant %v", got, want)
	}
}

func TestAngularFastifyExpressRules(t *testing.T) {
	angular := `import { Component } from '@angular/core';
export class C {
  load() {
    this.a.subscribe((x) => this.b.subscribe((y) => y));
    return this.sanitizer.bypassSecurityTrustHtml(this.html);
  }
}
`
	if got, want := findings(t, TypeScript{}, "src/c.component.ts", angular),
		[]string{"angular.bypass-security-trust:5", "angular.nested-subscribe:4"}; !reflect.DeepEqual(got, want) {
		t.Errorf("angular: got %v, want %v", got, want)
	}

	fastify := `import Fastify from 'fastify';
const app = Fastify();
app.post('/items', async (req, reply) => { reply.send({ ok: true }); });
app.post('/typed', { schema: { body: {} } }, async (req, reply) => { return { ok: true }; });
app.get('/items', async () => []);
`
	if got, want := findings(t, TypeScript{}, "src/server.ts", fastify),
		[]string{"fastify.async-reply-send:3", "fastify.route-without-schema:3"}; !reflect.DeepEqual(got, want) {
		t.Errorf("fastify: got %v, want %v", got, want)
	}

	express := `const express = require('express');
const app = express();
app.get('/hi', (req, res) => res.send(req.query.name));
app.get('/ok', (req, res) => res.json({ name: req.query.name }));
`
	if got, want := findings(t, JavaScript{}, "src/app.js", express), []string{"express.reflected-input:3"}; !reflect.DeepEqual(got, want) {
		t.Errorf("express: got %v, want %v", got, want)
	}
}

func TestFrameworkDetection(t *testing.T) {
	cases := map[string][]string{
		"import { Component } from '@angular/core';\n": {"angular"},
		"const f = require('fastify');\n":              {"fastify"},
		"import express from 'express';\n":             {"express"},
		"const cp = require('child_process');\n":       {},
	}
	for src, want := range cases {
		got := JavaScript{}.Frameworks(analysis.File{Path: "a.js", Content: []byte(src)})
		sort.Strings(got)
		if len(got) == 0 && len(want) == 0 {
			continue
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%q: got %v, want %v", src, got, want)
		}
	}
	if got := (TypeScript{}).Frameworks(analysis.File{Path: "a.tsx", Content: []byte("export const A = () => <div/>;\n")}); !reflect.DeepEqual(got, []string{"react"}) {
		t.Errorf("tsx should imply react, got %v", got)
	}
}
