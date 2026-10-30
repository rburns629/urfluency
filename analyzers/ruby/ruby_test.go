package ruby

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/rburns629/urfluency/analysis"
	"github.com/rburns629/urfluency/parsing"
	"github.com/rburns629/urfluency/rules"
)

func findings(t *testing.T, path, src string) []string {
	t.Helper()
	got, err := Analyzer{}.Analyze(context.Background(), []analysis.File{{Path: path, Content: []byte(src)}})
	if err != nil {
		t.Fatal(err)
	}
	out := []string{}
	for _, f := range got {
		if f.Positive {
			continue // idioms are tested separately
		}
		out = append(out, fmt.Sprintf("%s:%d", f.Rule, f.Line))
	}
	sort.Strings(out)
	return out
}

func TestRules(t *testing.T) {
	tests := []struct {
		name string
		path string
		src  string
		want []string
	}{
		{
			name: "eval of built strings and params",
			path: "app/models/a.rb",
			src: `class A
  def call(name, code)
    eval("@#{name} = 1")
    instance_eval "self." + name
    self.class.class_eval <<~RUBY
      def #{name}; end
    RUBY
    binding.eval(params[:code])
  end
end
`,
			want: []string{"ruby.eval-built:3", "ruby.eval-built:4", "ruby.eval-built:5", "ruby.eval-built:8"},
		},
		{
			name: "eval of literals, constants, blocks, and single-quoted heredocs",
			path: "app/models/a.rb",
			src: `class A
  eval("1 + 1")
  class_eval("def #{PREFIX}_name; end")
  instance_eval { @x = 1 }
  class_eval <<~'RUBY'
    def #{name}; end
  RUBY
  eval(code)
end
`,
			want: []string{},
		},
		{
			name: "send and constantize with params",
			path: "app/controllers/a_controller.rb",
			src: `class AController < ApplicationController
  def show
    record.send(params[:field])
    record.public_send("#{params[:attr]}_name")
    params[:type].constantize.new
    "#{params[:kind].classify}Handler".safe_constantize
    Object.const_get(request.params["klass"])
  end
end
`,
			want: []string{"ruby.dynamic-dispatch-from-params:3", "ruby.dynamic-dispatch-from-params:4",
				"ruby.dynamic-dispatch-from-params:5", "ruby.dynamic-dispatch-from-params:6", "ruby.dynamic-dispatch-from-params:7"},
		},
		{
			name: "send and constantize on names from code",
			path: "app/controllers/a_controller.rb",
			src: `class AController < ApplicationController
  HANDLERS = { "csv" => CsvExport }.freeze
  def show
    record.public_send(:name)
    record.send(attribute)
    HANDLERS.fetch(params[:format]).new
    "Admin::#{name}".constantize
  end
end
`,
			want: []string{},
		},
		{
			name: "YAML.load and Marshal.load",
			path: "lib/store.rb",
			src: `def read(data, path)
  YAML.load(data)
  Psych.unsafe_load(data)
  ::YAML.load(File.read(path))
  Marshal.load(data)
  Marshal.restore(Base64.decode64(data))
end
`,
			want: []string{"ruby.unsafe-deserialization:2", "ruby.unsafe-deserialization:3", "ruby.unsafe-deserialization:4",
				"ruby.unsafe-deserialization:5", "ruby.unsafe-deserialization:6"},
		},
		{
			name: "safe_load, load_file, JSON, and the Marshal deep copy",
			path: "lib/store.rb",
			src: `def read(data, obj)
  YAML.safe_load(data, permitted_classes: [Date])
  YAML.load_file("config/app.yml")
  JSON.parse(data)
  Marshal.load(Marshal.dump(obj))
  loader.load(data)
end
`,
			want: []string{},
		},
		{
			name: "commands built for the shell",
			path: "lib/deploy.rb",
			src: "def run(dir, ref)\n" +
				"  system(\"tar czf #{dir}.tgz #{dir}\")\n" +
				"  exec \"git checkout \" + ref\n" +
				"  `ls #{dir}`\n" +
				"  %x(rm -rf #{dir})\n" +
				"  Open3.capture2(\"git log #{ref}\")\n" +
				"  IO.popen(\"cat #{dir}/log\")\n" +
				"  Process.spawn({ \"A\" => \"1\" }, \"make #{ref}\", chdir: dir)\n" +
				"end\n",
			want: []string{"ruby.command-built:2", "ruby.command-built:3", "ruby.command-built:4", "ruby.command-built:5",
				"ruby.command-built:6", "ruby.command-built:7", "ruby.command-built:8"},
		},
		{
			name: "commands with separate arguments, escaping, or constants",
			path: "lib/deploy.rb",
			src: "def run(dir, ref)\n" +
				"  system(\"tar\", \"czf\", \"#{dir}.tgz\", dir)\n" +
				"  system(\"ls -la\")\n" +
				"  `git rev-parse #{ref.shellescape}`\n" +
				"  Open3.capture2(\"git\", \"log\", ref)\n" +
				"  system(\"make #{TARGET}\")\n" +
				"  conn.exec(\"SELECT #{ref}\")\n" +
				"end\n",
			want: []string{},
		},
		{
			name: "SQL built with interpolation and concatenation",
			path: "app/models/user.rb",
			src: `class User < ApplicationRecord
  scope :named, ->(name) { where("name = '#{name}'") }
  def self.search(q, sort)
    order("#{sort} DESC")
    find_by_sql("SELECT * FROM users WHERE email = '" + q + "'")
    connection.execute(<<~SQL)
      DELETE FROM users WHERE id = #{q}
    SQL
    User.joins("JOIN t ON t.id = #{q}").pluck(:id)
  end
end
`,
			want: []string{"ruby.sql-string-built:2", "ruby.sql-string-built:4", "ruby.sql-string-built:5",
				"ruby.sql-string-built:6", "ruby.sql-string-built:9"},
		},
		{
			name: "SQL with placeholders, hashes, table names, and migrations",
			path: "app/models/user.rb",
			src: `class User < ApplicationRecord
  def self.search(q, id)
    where("name LIKE ?", "%#{q}%")
    where(name: q).order(created_at: :desc)
    where("#{table_name}.id = ?", id)
    joins("JOIN #{Post.table_name} p ON p.user_id = users.id")
    where("id = #{id.to_i}")
    execute "UPDATE users SET admin = false WHERE role = '#{role}'"
    order("name")
    items.select { |i| i.ok? }
  end
end
`,
			want: []string{},
		},
		{
			name: "html_safe and raw on interpolated values",
			path: "app/helpers/a_helper.rb",
			src: `module AHelper
  def badge(user)
    "<b>#{user.name}</b>".html_safe
    raw("<i>#{params[:q]}</i>")
    params[:bio].html_safe
    ("<p>" + user.bio + "</p>").html_safe
  end
end
`,
			want: []string{"ruby.html-safe-built:3", "ruby.html-safe-built:4", "ruby.html-safe-built:5", "ruby.html-safe-built:6"},
		},
		{
			name: "html_safe on literals and escaped values",
			path: "app/helpers/a_helper.rb",
			src: `module AHelper
  def badge(user)
    "<br>".html_safe
    "<b>#{h(user.name)}</b>".html_safe
    "#{link_to('Home', root_path)} &middot;".html_safe
    raw(t("footer_html"))
    content_tag(:b, user.name)
  end
end
`,
			want: []string{},
		},
		{
			name: "TLS verification disabled",
			path: "lib/client.rb",
			src: `def connect(http)
  http.verify_mode = OpenSSL::SSL::VERIFY_NONE
  Net::HTTP.start(host, 443, use_ssl: true, verify_mode: ::OpenSSL::SSL::VERIFY_NONE)
  http.verify_mode = OpenSSL::SSL::VERIFY_PEER
end
`,
			want: []string{"ruby.ssl-verify-none:2", "ruby.ssl-verify-none:3"},
		},
		{
			name: "Kernel#open on values",
			path: "lib/fetch.rb",
			src: `def fetch(url, name)
  open(url).read
  Kernel.open(params[:file]) { |f| f.read }
  open("#{name}.txt", "w")
end
`,
			want: []string{"ruby.kernel-open:2", "ruby.kernel-open:3", "ruby.kernel-open:4"},
		},
		{
			name: "File.open, URI.open, and open on literal paths",
			path: "lib/fetch.rb",
			src: `def fetch(url, name)
  File.open(name)
  URI.open(url)
  open("config/app.yml")
  open("/tmp/#{name}.txt", "w")
  open(File.join(DIR, name))
  open(CONFIG_PATH)
  file.open
end
`,
			want: []string{},
		},
		{
			name: "rescue Exception, empty rescues, and rescue modifiers",
			path: "lib/job.rb",
			src: `def perform
  run
rescue Exception => e
  log(e)
end

def retry_it
  begin
    run
  rescue
  end
  begin
    run
  rescue Errno::ENOENT, ::Exception
  end
  value = Integer(input) rescue nil
end
`,
			want: []string{"ruby.empty-rescue:10", "ruby.empty-rescue:14", "ruby.rescue-exception:14",
				"ruby.rescue-exception:3", "ruby.rescue-modifier:16"},
		},
		{
			name: "rescues that handle, explain, or re-raise",
			path: "lib/job.rb",
			src: `def perform
  run
rescue StandardError => e
  log(e)
rescue Errno::ENOENT
  # the cache file is optional
rescue Timeout::Error => _error
rescue Exception => e
  cleanup
  raise
end
`,
			want: []string{},
		},
		{
			name: "rescues in specs",
			path: "spec/models/user_spec.rb",
			src:  "it 'works' do\n  begin\n    run\n  rescue Exception\n  end\n  x = run rescue nil\nend\n",
			want: []string{},
		},
		{
			name: "global variables, but not Ruby's own",
			path: "lib/config.rb",
			src: `$redis = Redis.new
$count ||= 0
$stdout = StringIO.new
$VERBOSE = nil
$LOAD_PATH << "lib"
puts $redis
`,
			want: []string{"ruby.global-variable:1", "ruby.global-variable:2"},
		},
		{
			name: "core classes reopened or patched",
			path: "lib/core_ext.rb",
			src: `class String
  def shout = upcase
end
class ::Hash; end
module Kernel; end
Array.include(Extensions)
Integer.class_eval { def double = self * 2 }
`,
			want: []string{"ruby.core-class-reopened:1", "ruby.core-class-reopened:4", "ruby.core-class-reopened:5",
				"ruby.core-class-reopened:6", "ruby.core-class-reopened:7"},
		},
		{
			name: "namespaced classes, subclasses, and refinements",
			path: "lib/core_ext.rb",
			src: `module App
  class String; end
  refine Array do
    def second = self[1]
  end
end
class Money < Struct.new(:cents); end
class Error < StandardError; end
[1].include?(1)
`,
			want: []string{},
		},
		{
			name: "debuggers and debug prints",
			path: "app/models/order.rb",
			src: `class Order
  def total
    binding.pry
    byebug
    debugger if ENV["DEBUG"]
    binding.irb
    p items
    pp(total: 1)
    items.sum
  end
end
`,
			want: []string{"ruby.debugging-call:3", "ruby.debugging-call:4", "ruby.debugging-call:5", "ruby.debugging-call:6",
				"ruby.debugging-call:7", "ruby.debugging-call:8"},
		},
		{
			name: "HTML builders, variables named debugger, and other binding calls",
			path: "app/views/home.rb",
			src: `class Home < Phlex::HTML
  def view_template = p(class: "lead") { "Hi" }
end
debugger = Debugger.new
binding.local_variable_get(:x)
`,
			want: []string{},
		},
		{
			name: "prints outside app and lib",
			path: "script/report.rb",
			src:  "p totals\npp rows\n",
			want: []string{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := findings(t, tt.path, tt.src); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRulesAreInCatalog(t *testing.T) {
	ids := []string{"ruby.eval-built", "ruby.dynamic-dispatch-from-params", "ruby.unsafe-deserialization",
		"ruby.command-built", "ruby.sql-string-built", "ruby.html-safe-built", "ruby.ssl-verify-none", "ruby.kernel-open",
		"ruby.rescue-exception", "ruby.empty-rescue", "ruby.rescue-modifier", "ruby.global-variable",
		"ruby.core-class-reopened", "ruby.debugging-call"}
	for _, id := range ids {
		r, ok := rules.Lookup(id)
		if !ok || r.Category == rules.Idiom {
			t.Errorf("%s is not a rule in the catalog", id)
			continue
		}
		found := false
		for _, l := range r.Languages {
			found = found || l == "ruby"
		}
		if !found {
			t.Errorf("%s does not list ruby", id)
		}
	}
	if _, ok := rules.Lookup("clean.cognitive-complexity"); !ok {
		t.Error("clean.cognitive-complexity is not in the catalog")
	}
}

func TestCognitiveComplexity(t *testing.T) {
	// Each nested structure adds 1 plus its depth: if 1, each block 0 (nests),
	// while 3, case 4, modifier if 5 = 13, plus elsif and else 2 = 15, at the
	// threshold; the && in the deepest condition takes it over.
	body := func(cond string) string {
		return `def deep(a, b, items)
  if a
    items.each do |i|
      while b
        case i
        when 1 then puts(i) if ` + cond + `
        end
      end
    end
  elsif b
    1
  else
    2
  end
end

def simple(a) = a ? 1 : 2
`
	}
	if got := findings(t, "lib/a.rb", body("a && b")); !reflect.DeepEqual(got, []string{"clean.cognitive-complexity:1"}) {
		t.Errorf("complex method: got %v", got)
	}
	if got := findings(t, "lib/a.rb", body("a")); len(got) != 0 {
		t.Errorf("method at the threshold: got %v", got)
	}
	if got := findings(t, "spec/deep_spec.rb", body("a && b")); len(got) != 0 {
		t.Errorf("spec file: got %v", got)
	}
}

func TestCognitiveScores(t *testing.T) {
	// Scores are checked directly so each construct's weight is pinned.
	tests := []struct {
		name string
		src  string
		want int
	}{
		{"modifiers, unless, and ternary", `def f(a, b)
  return if a
  unless b
    x = a ? 1 : 2
  end
end
`, 1 + 1 + 2},
		{"rescue, until, for, and mixed logical operators", `def f(a, b, c)
  for x in a
    until b
      y = a && b || c
    end
  end
rescue IOError
  retry
end
`, 1 + 2 + 2 + 1},
		{"case/in and and/or sequences", `def f(v, a, b, c)
  case v
  in [x, *] if a and b and c
    x
  in {y:}
    y
  end
  a or b or c
end
`, 1 + 1 + 1},
		{"blocks nest", `def f(items)
  items.map do |i|
    items.select { |j| j if i }
  end
end
`, 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := cognitive(t, tt.src)
			if len(got) != 1 || got[0] != tt.want {
				t.Errorf("got %v, want [%d]", got, tt.want)
			}
		})
	}
}

// cognitive returns the complexity of each outermost method in src.
func cognitive(t *testing.T, src string) []int {
	t.Helper()
	tree, err := parsing.Parse(parsing.Ruby, []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	defer tree.Close()
	var out []int
	for _, fn := range parsing.Cognitive(tree, complexity) {
		out = append(out, fn.Complexity)
	}
	return out
}

func TestSkip(t *testing.T) {
	tests := []struct {
		path, src string
		want      bool
	}{
		{"app/models/user.rb", "class User; end\n", false},
		{"tmp/cache/bootsnap/a.rb", "x = 1\n", true},
		{"db/schema.rb", "# This file is auto-generated from the current state of the database.\nActiveRecord::Schema.define {}\n", true},
		{"lib/a.rb", "# Code generated by protoc-gen-twirp_ruby. DO NOT EDIT.\nmodule A; end\n", true},
		{"lib/a.rb", "class A\n  # @generatedvalue is a column\nend\n", false},
		{"lib/a.rb", "x = 1\n" + strings.Repeat("\n", 600) + "# DO NOT EDIT below\n", false},
	}
	for _, tt := range tests {
		if got := (Analyzer{}).Skip(analysis.File{Path: tt.path, Content: []byte(tt.src)}); got != tt.want {
			t.Errorf("Skip(%s, %.30q) = %v, want %v", tt.path, tt.src, got, tt.want)
		}
	}
	if !(Analyzer{}).SkipPath("tmp/a.rb") || (Analyzer{}).SkipPath("lib/tmpfiles/a.rb") {
		t.Error("SkipPath must exclude tmp/ and only tmp/")
	}
}

func TestFrameworks(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want []string
	}{
		{"requires", `require "rails/all"
require "sinatra/base"
require "hanami/api"
require 'roda'
require "rspec/core"
require "minitest/autorun"
require "sidekiq"
require "sequel"
require "grape"
require "json"
require_relative "rails"
`, []string{"grape", "hanami", "minitest", "rails", "roda", "rspec", "sequel", "sidekiq", "sinatra"}},
		{"Rails constants", `class User < ApplicationRecord; end
class Post < ActiveRecord::Base; end
`, []string{"rails"}},
		{"Rails application", "Rails.application.configure do\nend\n", []string{"rails"}},
		{"Rails controller", "class HomeController < ApplicationController\nend\n", []string{"rails"}},
		{"Sinatra and Roda apps", "class Api < Sinatra::Base; end\nclass Web < Roda; end\n", []string{"roda", "sinatra"}},
		{"Hanami action", "module Web\n  class Show < Hanami::Action; end\nend\n", []string{"hanami"}},
		{"RSpec", "require \"rails_helper\"\nRSpec.describe User do\nend\n", []string{"rails", "rspec"}},
		{"plain Ruby and look-alikes", `require "json"
module MyGem
  class Rails; end
  Config = Struct.new(:rails)
end
x = MyGem::Rails.new
`, []string{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Analyzer{}.Frameworks(analysis.File{Path: "a.rb", Content: []byte(tt.src)})
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRegistered(t *testing.T) {
	for _, p := range []string{"app/models/user.rb", "lib/tasks/db.rake", "my_gem.gemspec", "config.ru"} {
		if a := analysis.ForPath(p); a == nil || a.Language() != "ruby" {
			t.Errorf("ForPath(%s) = %v", p, a)
		}
	}
}

func TestIsTestPath(t *testing.T) {
	for p, want := range map[string]bool{
		"spec/models/user_spec.rb": true, "test/models/user_test.rb": true, "lib/user_spec.rb": true,
		"app/models/user.rb": false, "lib/spectrum.rb": false,
	} {
		if got := analysis.IsTestPath(p); got != want {
			t.Errorf("IsTestPath(%s) = %v, want %v", p, got, want)
		}
	}
}
