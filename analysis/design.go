package analysis

import (
	"go/parser"
	"go/token"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Usage is how many repositories and owned files used something.
type Usage struct {
	Repos int
	Files int
}

// DesignUse is how much owned code worked in one area of design, such as cloud
// services or messaging, judged from the imports of files with owned lines.
// Libraries counts files per library; only library names are kept. Languages
// counts the owned lines of those files per language, and Months the months
// in which they were committed (empty without Git).
type DesignUse struct {
	Usage
	Libraries map[string]int
	Languages map[string]int
	Months    Months
}

// Months is a set of months, as "2006-01", in which the user committed work.
type Months map[string]bool

// Add records the month of t; a zero time is ignored.
func (m Months) Add(t time.Time) {
	if !t.IsZero() {
		m[t.UTC().Format("2006-01")] = true
	}
}

// Union adds every month of o.
func (m Months) Union(o Months) {
	for k := range o {
		m[k] = true
	}
}

// DesignAreas names each design area, in report order.
var DesignAreas = []struct{ ID, Title string }{
	{"cloud", "Cloud services"},
	{"integration", "Integrations with other services"},
	{"api", "Web APIs and services"},
	{"messaging", "Messaging and events"},
	{"data", "Databases and storage"},
	{"analytics", "Data processing and analytics"},
	{"auth", "Authentication and identity"},
	{"observability", "Logging, metrics, and tracing"},
	{"infrastructure", "Infrastructure as code"},
	{"ai", "AI and LLM applications"},
	{"ml", "Machine learning"},
	{"frontend", "Web front ends"},
	{"mobile", "Mobile apps"},
	{"cli", "Command-line tools"},
}

// designLibraries maps import prefixes to a design area and the library name
// reported. A prefix ending in "/", ".", "_", or ":" matches any import starting
// with it; any other prefix matches the import itself or its subpackages.
// Imports are lowercased first, so C# and Swift names match too.
var designLibraries = []struct {
	area, library string
	prefixes      []string
}{
	{"cloud", "gcp", []string{"cloud.google.com/go", "google.cloud", "google.api_core", "googleapiclient", "@google-cloud/", "com.google.cloud", "functions_framework", "@google-cloud/functions-framework"}},
	{"cloud", "aws", []string{"github.com/aws/aws-sdk-go", "github.com/aws/aws-sdk-go-v2", "github.com/aws/aws-lambda-go", "boto3", "botocore", "@aws-sdk/", "aws-sdk", "aws-lambda", "software.amazon.awssdk", "com.amazonaws", "amazon.", "aws_sdk_", "aws_config", "aws_lambda_powertools"}},
	{"cloud", "azure", []string{"github.com/azure/azure-sdk-for-go", "azure.", "@azure/", "com.azure", "azure_", "microsoft.azure"}},
	{"cloud", "firebase", []string{"firebase", "@firebase/", "firebase_admin", "firebase-admin", "firebase-functions", "@react-native-firebase/", "package:firebase_", "package:cloud_firestore", "com.google.firebase", "firebase.google.com/go", "firebasecore", "firebaseauth", "firebasefirestore"}},
	{"cloud", "cloudflare", []string{"@cloudflare/", "cloudflare"}},
	{"cloud", "vercel", []string{"@vercel/"}},

	{"messaging", "pubsub", []string{"cloud.google.com/go/pubsub", "google.cloud.pubsub", "google.cloud.pubsub_v1", "@google-cloud/pubsub", "com.google.cloud.pubsub", "google.cloud.pubsub.v1", "firebase-functions/v2/pubsub", "firebase-functions/v1/pubsub"}},
	{"messaging", "kafka", []string{"kafka", "kafkajs", "confluent_kafka", "github.com/segmentio/kafka-go", "github.com/confluentinc/", "github.com/ibm/sarama", "github.com/shopify/sarama", "org.apache.kafka", "rdkafka", "confluent.kafka"}},
	{"messaging", "rabbitmq", []string{"pika", "aio_pika", "amqplib", "amqp", "github.com/rabbitmq/", "github.com/streadway/amqp", "com.rabbitmq", "rabbitmq.client", "lapin"}},
	{"messaging", "nats", []string{"github.com/nats-io/", "nats"}},
	{"messaging", "sqs-sns", []string{"@aws-sdk/client-sqs", "@aws-sdk/client-sns", "github.com/aws/aws-sdk-go-v2/service/sqs", "github.com/aws/aws-sdk-go-v2/service/sns", "aws_sdk_sqs", "aws_sdk_sns"}},
	{"messaging", "celery", []string{"celery"}},
	{"messaging", "bullmq", []string{"bullmq", "bull"}},
	{"messaging", "cloud-events", []string{"cloudevents", "@google/events", "github.com/cloudevents/"}},

	{"api", "flask", []string{"flask"}},
	{"api", "fastapi", []string{"fastapi", "starlette"}},
	{"api", "django", []string{"django", "rest_framework"}},
	{"api", "express", []string{"express"}},
	{"api", "fastify", []string{"fastify"}},
	{"api", "nestjs", []string{"@nestjs/"}},
	{"api", "koa", []string{"koa", "@koa/"}},
	{"api", "hapi", []string{"@hapi/"}},
	{"api", "cloud-functions", []string{"firebase-functions", "functions_framework", "@google-cloud/functions-framework", "github.com/googlecloudplatform/functions-framework-go", "azure.functions", "@azure/functions", "github.com/aws/aws-lambda-go", "aws-lambda"}},
	{"api", "fiber", []string{"github.com/gofiber/fiber"}},
	{"api", "gin", []string{"github.com/gin-gonic/gin"}},
	{"api", "echo", []string{"github.com/labstack/echo"}},
	{"api", "chi", []string{"github.com/go-chi/chi"}},
	{"api", "gorilla", []string{"github.com/gorilla/mux"}},
	{"api", "grpc", []string{"google.golang.org/grpc", "grpc", "@grpc/grpc-js", "io.grpc", "grpc.core", "tonic"}},
	{"api", "spring", []string{"org.springframework.web", "org.springframework.boot"}},
	{"api", "jax-rs", []string{"javax.ws.rs", "jakarta.ws.rs"}},
	{"api", "micronaut", []string{"io.micronaut.http"}},
	{"api", "quarkus", []string{"io.quarkus"}},
	{"api", "ktor", []string{"io.ktor.server"}},
	{"api", "asp.net-core", []string{"microsoft.aspnetcore"}},
	{"api", "actix", []string{"actix_web"}},
	{"api", "axum", []string{"axum"}},
	{"api", "rocket", []string{"rocket"}},
	{"api", "vapor", []string{"vapor"}},
	{"api", "shelf", []string{"package:shelf"}},
	{"api", "graphql", []string{"graphql", "@apollo/server", "apollo-server", "graphene", "strawberry", "github.com/99designs/gqlgen", "github.com/graphql-go/"}},

	{"integration", "http-client", []string{"requests", "httpx", "aiohttp", "urllib3", "axios", "node-fetch", "got", "ky", "superagent", "undici", "github.com/go-resty/", "github.com/hashicorp/go-retryablehttp", "okhttp3", "retrofit2", "java.net.http", "io.ktor.client", "system.net.http", "reqwest", "package:http/", "package:dio/", "alamofire", "curl/"}},
	{"integration", "google-workspace", []string{"googleapiclient", "gspread", "googleapis", "google.golang.org/api", "@googleapis/", "com.google.api.services"}},
	{"integration", "slack", []string{"slack_sdk", "slack_bolt", "slackclient", "@slack/", "github.com/slack-go/"}},
	{"integration", "salesforce", []string{"simple_salesforce", "jsforce"}},
	{"integration", "atlassian", []string{"jira", "atlassian", "jira-client", "github.com/andygrunwald/go-jira"}},
	{"integration", "github", []string{"github", "@octokit/", "github.com/google/go-github"}},
	{"integration", "stripe", []string{"stripe", "github.com/stripe/"}},
	{"integration", "twilio", []string{"twilio"}},
	{"integration", "sendgrid", []string{"sendgrid", "@sendgrid/"}},
	{"integration", "okta", []string{"okta", "@okta/"}},
	{"integration", "servicenow", []string{"pysnow", "servicenow"}},
	{"integration", "hubspot", []string{"hubspot", "@hubspot/"}},
	{"integration", "zendesk", []string{"zenpy", "node-zendesk"}},
	{"integration", "microsoft-graph", []string{"msgraph", "@microsoft/microsoft-graph-client", "microsoft.graph"}},

	{"data", "sql", []string{"database/sql", "github.com/jackc/pgx", "github.com/lib/pq", "github.com/go-sql-driver/mysql", "github.com/jmoiron/sqlx", "gorm.io/", "sqlalchemy", "psycopg", "psycopg2", "pymysql", "mysql", "sqlite3", "asyncpg", "django.db", "pg", "mysql2", "sequelize", "typeorm", "@prisma/client", "knex", "drizzle-orm", "better-sqlite3", "java.sql", "org.hibernate", "jakarta.persistence", "javax.persistence", "org.springframework.data", "org.springframework.jdbc", "microsoft.entityframeworkcore", "system.data", "dapper", "diesel", "sqlx", "rusqlite", "package:sqflite", "org.jetbrains.exposed", "androidx.room", "sqlite3.h", "libpq-fe.h", "grdb", "coredata", "swiftdata"}},
	{"data", "mongodb", []string{"pymongo", "motor", "mongoose", "mongodb", "go.mongodb.org/", "org.mongodb", "com.mongodb", "mongodb.driver"}},
	{"data", "redis", []string{"redis", "ioredis", "github.com/redis/go-redis", "github.com/go-redis/", "github.com/gomodule/redigo", "stackexchange.redis", "io.lettuce", "redis.clients"}},
	{"data", "firestore", []string{"google.cloud.firestore", "cloud.google.com/go/firestore", "@google-cloud/firestore", "firebase/firestore", "firebase_admin.firestore", "package:cloud_firestore", "@react-native-firebase/firestore", "com.google.cloud.firestore", "firebasefirestore"}},
	{"data", "dynamodb", []string{"@aws-sdk/client-dynamodb", "@aws-sdk/lib-dynamodb", "github.com/aws/aws-sdk-go-v2/service/dynamodb", "aws_sdk_dynamodb", "pynamodb"}},
	{"data", "elasticsearch", []string{"elasticsearch", "@elastic/", "github.com/elastic/go-elasticsearch", "opensearchpy", "@opensearch-project/"}},
	{"data", "object-storage", []string{"cloud.google.com/go/storage", "google.cloud.storage", "@google-cloud/storage", "@aws-sdk/client-s3", "github.com/aws/aws-sdk-go-v2/service/s3", "azure.storage", "@azure/storage-blob", "minio"}},
	{"data", "spanner", []string{"cloud.google.com/go/spanner", "google.cloud.spanner", "@google-cloud/spanner"}},

	{"analytics", "bigquery", []string{"cloud.google.com/go/bigquery", "google.cloud.bigquery", "@google-cloud/bigquery", "com.google.cloud.bigquery"}},
	{"analytics", "pandas", []string{"pandas", "polars", "pyarrow"}},
	{"analytics", "numpy", []string{"numpy", "scipy"}},
	{"analytics", "spark", []string{"pyspark", "org.apache.spark"}},
	{"analytics", "beam", []string{"apache_beam", "org.apache.beam", "github.com/apache/beam"}},
	{"analytics", "airflow", []string{"airflow"}},
	{"analytics", "snowflake", []string{"snowflake", "snowflake-sdk"}},
	{"analytics", "dbt", []string{"dbt"}},
	{"analytics", "charts", []string{"matplotlib", "plotly", "seaborn", "chart.js", "recharts", "d3", "echarts", "package:fl_chart"}},

	{"auth", "jwt", []string{"jwt", "jose", "jsonwebtoken", "github.com/golang-jwt/", "github.com/dgrijalva/jwt-go", "github.com/lestrrat-go/jwx", "io.jsonwebtoken", "jsonwebtoken", "system.identitymodel.tokens.jwt"}},
	{"auth", "oauth", []string{"golang.org/x/oauth2", "google.oauth2", "google.auth", "google_auth_oauthlib", "authlib", "oauthlib", "requests_oauthlib", "google-auth-library", "passport", "passport-", "next-auth", "@auth0/", "auth0", "msal", "@azure/msal-", "microsoft.identity", "org.springframework.security", "microsoft.aspnetcore.authentication", "keycloak", "@okta/okta-auth-js"}},
	{"auth", "firebase-auth", []string{"firebase/auth", "firebase_admin.auth", "@react-native-firebase/auth", "package:firebase_auth", "firebaseauth"}},
	{"auth", "crypto", []string{"bcrypt", "argon2", "passlib", "golang.org/x/crypto"}},

	{"observability", "opentelemetry", []string{"opentelemetry", "@opentelemetry/", "go.opentelemetry.io/", "io.opentelemetry"}},
	{"observability", "prometheus", []string{"prometheus_client", "prom-client", "github.com/prometheus/"}},
	{"observability", "sentry", []string{"sentry_sdk", "@sentry/", "github.com/getsentry/", "io.sentry", "sentry"}},
	{"observability", "datadog", []string{"ddtrace", "dd-trace", "datadog", "gopkg.in/datadog/", "github.com/datadog/"}},
	{"observability", "cloud-logging", []string{"cloud.google.com/go/logging", "google.cloud.logging", "@google-cloud/logging", "@google-cloud/logging-winston", "watchtower"}},
	{"observability", "structured-logging", []string{"structlog", "loguru", "go.uber.org/zap", "github.com/sirupsen/logrus", "github.com/rs/zerolog", "log/slog", "winston", "pino", "serilog", "tracing", "org.slf4j"}},

	{"infrastructure", "pulumi", []string{"pulumi", "@pulumi/", "github.com/pulumi/"}},
	{"infrastructure", "cdk", []string{"aws_cdk", "aws-cdk-lib", "constructs", "cdktf"}},
	{"infrastructure", "kubernetes-client", []string{"kubernetes", "k8s.io/", "@kubernetes/client-node", "io.fabric8", "kube"}},
	{"infrastructure", "docker-sdk", []string{"docker", "dockerode", "github.com/docker/"}},

	{"ai", "openai", []string{"openai", "github.com/sashabaranov/go-openai", "github.com/openai/"}},
	{"ai", "anthropic", []string{"anthropic", "@anthropic-ai/", "github.com/anthropics/"}},
	{"ai", "gemini", []string{"google.generativeai", "google.genai", "vertexai", "@google/generative-ai", "@google/genai", "@google-cloud/vertexai", "google.golang.org/genai", "cloud.google.com/go/vertexai"}},
	{"ai", "langchain", []string{"langchain", "langchain_core", "langchain_community", "langchain_openai", "@langchain/", "langgraph", "llama_index"}},
	{"ai", "ollama", []string{"ollama", "github.com/ollama/"}},
	{"ai", "mcp", []string{"mcp", "@modelcontextprotocol/", "github.com/mark3labs/mcp-go", "github.com/modelcontextprotocol/"}},

	{"ml", "scikit-learn", []string{"sklearn"}},
	{"ml", "pytorch", []string{"torch", "torchvision", "lightning"}},
	{"ml", "tensorflow", []string{"tensorflow", "keras", "@tensorflow/", "package:tflite_flutter", "org.tensorflow"}},
	{"ml", "transformers", []string{"transformers", "sentence_transformers", "@huggingface/", "huggingface_hub"}},
	{"ml", "gradient-boosting", []string{"xgboost", "lightgbm", "catboost"}},
	{"ml", "coreml", []string{"coreml", "createml", "vision"}},
	{"ml", "opencv", []string{"cv2", "opencv2/", "org.opencv"}},

	{"frontend", "react", []string{"react", "react-dom", "react-router", "react-router-dom", "@tanstack/react-query"}},
	{"frontend", "next.js", []string{"next"}},
	{"frontend", "vue", []string{"vue", "nuxt", "pinia"}},
	{"frontend", "angular", []string{"@angular/"}},
	{"frontend", "svelte", []string{"svelte", "@sveltejs/"}},
	{"frontend", "ui-kits", []string{"@mui/", "@chakra-ui/", "antd", "@radix-ui/", "@headlessui/", "bootstrap", "react-bootstrap"}},
	{"frontend", "jquery", []string{"jquery"}},

	{"mobile", "react-native", []string{"react-native", "react-native-", "expo", "expo-", "@react-navigation/", "@react-native-", "@expo/"}},
	{"mobile", "flutter", []string{"package:flutter/", "package:flutter_"}},
	{"mobile", "ios", []string{"uikit", "swiftui"}},
	{"mobile", "android", []string{"android.", "androidx."}},
	{"mobile", "maui", []string{"microsoft.maui"}},

	{"cli", "click", []string{"click", "typer"}},
	{"cli", "argparse", []string{"argparse", "fire", "docopt"}},
	{"cli", "cobra", []string{"github.com/spf13/cobra", "github.com/urfave/cli", "github.com/alecthomas/kong"}},
	{"cli", "node-cli", []string{"commander", "yargs", "inquirer", "@oclif/", "meow"}},
	{"cli", "clap", []string{"clap"}},
	{"cli", "picocli", []string{"picocli", "info.picocli"}},
	{"cli", "system.commandline", []string{"system.commandline"}},
	{"cli", "swift-argument-parser", []string{"argumentparser"}},
	// PHP namespaces (Aws\S3 is read as aws/s3) and Ruby gems by their require path.
	{"cloud", "aws", []string{"aws/", "aws-sdk-", "aws-sdk"}},
	{"cloud", "gcp", []string{"google/cloud/", "google/cloud-", "google-cloud-"}},
	{"cloud", "azure", []string{"microsoftazure/", "azure-storage-", "azure/"}},
	{"cloud", "firebase", []string{"kreait/firebase/", "firebase/firebase", "firebase-admin"}},
	{"messaging", "rabbitmq", []string{"phpamqplib/", "bunny/", "bunny", "sneakers"}},
	{"messaging", "background-jobs", []string{"sidekiq", "resque", "good_job", "solid_queue", "delayed_job", "illuminate/queue/", "illuminate/bus/"}},
	{"messaging", "kafka", []string{"rdkafka", "ruby-kafka", "karafka"}},
	{"data", "sql", []string{"doctrine/dbal/", "doctrine/orm/", "illuminate/database/", "pg", "mysql2", "sqlite3", "sequel", "active_record", "activerecord"}},
	{"data", "redis", []string{"predis/", "redis"}},
	{"data", "mongodb", []string{"mongodb/", "mongo", "mongoid"}},
	{"data", "elasticsearch", []string{"elastic/elasticsearch/", "elasticsearch"}},
	{"integration", "http-client", []string{"guzzlehttp/", "symfony/component/httpclient/", "symfony/contracts/httpclient/", "faraday", "httparty", "excon", "http.rb"}},
	{"integration", "stripe", []string{"stripe/", "stripe"}},
	{"integration", "twilio", []string{"twilio/", "twilio-ruby"}},
	{"integration", "slack", []string{"slack-ruby-client", "jolicode/slack/"}},
	{"api", "laravel", []string{"illuminate/http/", "illuminate/routing/", "illuminate/support/facades/route"}},
	{"api", "symfony", []string{"symfony/component/httpfoundation/", "symfony/component/routing/", "symfony/bundle/frameworkbundle/"}},
	{"api", "slim", []string{"slim/"}},
	{"api", "rails", []string{"rails", "action_controller", "rails/all"}},
	{"api", "sinatra", []string{"sinatra", "sinatra/base"}},
	{"api", "grape", []string{"grape"}},
	{"api", "graphql", []string{"graphql", "graphql/", "nuwave/lighthouse/"}},
	{"auth", "jwt", []string{"firebase/jwt/", "lcobucci/jwt/", "jwt"}},
	{"auth", "oauth", []string{"league/oauth2/", "laravel/socialite/", "laravel/passport/", "omniauth", "omniauth-", "doorkeeper", "oauth2"}},
	{"auth", "devise", []string{"devise"}},
	{"observability", "structured-logging", []string{"monolog/", "psr/log/", "semantic_logger", "lograge"}},
	{"observability", "sentry", []string{"sentry/", "sentry-ruby", "sentry-rails"}},
	{"observability", "opentelemetry", []string{"opentelemetry/", "opentelemetry-", "opentelemetry/sdk"}},
	{"observability", "datadog", []string{"ddtrace", "datadog"}},
	{"ai", "openai", []string{"openai/", "openai"}},
	{"ai", "anthropic", []string{"anthropic/", "anthropic"}},
	{"cli", "symfony-console", []string{"symfony/component/console/"}},
	{"cli", "thor", []string{"thor"}},
	{"frontend", "livewire", []string{"livewire/"}},
	{"frontend", "hotwire", []string{"turbo-rails", "stimulus-rails"}},
	{"infrastructure", "docker-sdk", []string{"docker-api", "docker/"}},
}

// designPatterns find design areas that code shows without importing a
// client library: functions triggered by Pub/Sub, CloudEvent handlers, and
// push-subscription endpoints, which receive messages as plain HTTP requests.
var designPatterns = []struct {
	area, library string
	re            *regexp.Regexp
}{
	{"messaging", "pubsub", regexp.MustCompile(`\bfunctions\.pubsub\.topic\(|\bonMessagePublished\(`)},
	// A push subscription posts {"message": {"data": ...}, "subscription": ...}.
	{"messaging", "pubsub", regexp.MustCompile(`\[\s*["']message["']\s*\]\s*\[\s*["']data["']\s*\]|\b(?:req|request)\.body\.message\.data\b|json:"subscription"`)},
	{"messaging", "cloud-events", regexp.MustCompile(`@functions_framework\.cloud_event\b|\bfunctions\.(?:cloudEvent|CloudEvent)\(`)},
}

// designInContent returns the design areas and libraries designPatterns find.
func designInContent(content []byte) map[string]map[string]bool {
	out := map[string]map[string]bool{}
	for _, p := range designPatterns {
		if p.re.Match(content) {
			if out[p.area] == nil {
				out[p.area] = map[string]bool{}
			}
			out[p.area][p.library] = true
		}
	}
	return out
}

// importPatterns extract the imported module of each import statement. Go is
// read with its parser instead.
var importPatterns = map[string]*regexp.Regexp{
	"python":     regexp.MustCompile(`(?m)^[ \t]*(?:from[ \t]+([\w.]+)[ \t]+import\b|import[ \t]+([\w., \t]+))`),
	"javascript": regexp.MustCompile(`(?m)(?:\bfrom[ \t]*|^[ \t]*import[ \t]*|\brequire\([ \t]*|\bimport\([ \t]*)['"]([^'"\n]+)['"]`),
	"typescript": regexp.MustCompile(`(?m)(?:\bfrom[ \t]*|^[ \t]*import[ \t]*|\brequire\([ \t]*|\bimport\([ \t]*)['"]([^'"\n]+)['"]`),
	"java":       regexp.MustCompile(`(?m)^[ \t]*import[ \t]+(?:static[ \t]+)?([\w.]+)`),
	"kotlin":     regexp.MustCompile(`(?m)^[ \t]*import[ \t]+([\w.]+)`),
	"csharp":     regexp.MustCompile(`(?m)^[ \t]*(?:global[ \t]+)?using[ \t]+(?:static[ \t]+)?([\w.]+)[ \t]*;`),
	"rust":       regexp.MustCompile(`(?m)^[ \t]*(?:pub(?:\([\w ]+\))?[ \t]+)?(?:use|extern[ \t]+crate)[ \t]+(?:::)?([\w:]+)`),
	"c":          regexp.MustCompile(`(?m)^[ \t]*#[ \t]*include[ \t]*[<"]([^>"\n]+)[>"]`),
	"cpp":        regexp.MustCompile(`(?m)^[ \t]*#[ \t]*include[ \t]*[<"]([^>"\n]+)[>"]`),
	"swift":      regexp.MustCompile(`(?m)^[ \t]*(?:@\w+[ \t]+)*import[ \t]+(?:(?:class|struct|enum|protocol|func|var|let|typealias)[ \t]+)?([\w.]+)`),
	"dart":       regexp.MustCompile(`(?m)^[ \t]*(?:import|export)[ \t]+['"]([^'"\n]+)['"]`),
	// PHP use statements, including function and const imports and the
	// shared prefix of a group use (use A\{B, C}).
	"php": regexp.MustCompile(`(?m)^[ \t]*use[ \t]+(?:function[ \t]+|const[ \t]+)?\\?([\w\\]+)`),
	// Ruby require; require_relative names the project's own files.
	"ruby": regexp.MustCompile(`(?m)^[ \t]*require[ \t(]*['"]([^'"\n]+)['"]`),
}

// Imports returns the lowercased modules a file imports, or nil for a language
// without import detection. Relative imports are left out: they name the
// user's own code, not a library.
func Imports(language string, f File) []string {
	var raw []string
	if language == "go" {
		file, err := parser.ParseFile(token.NewFileSet(), f.Path, f.Content, parser.ImportsOnly)
		if err != nil {
			return nil
		}
		for _, imp := range file.Imports {
			if p, err := strconv.Unquote(imp.Path.Value); err == nil {
				raw = append(raw, p)
			}
		}
	} else if re := importPatterns[language]; re != nil {
		for _, m := range re.FindAllSubmatch(f.Content, -1) {
			for _, g := range m[1:] {
				if language == "python" {
					// "import a, b as c" names several modules.
					for _, part := range strings.Split(string(g), ",") {
						if fields := strings.Fields(part); len(fields) > 0 {
							raw = append(raw, fields[0])
						}
					}
				} else if len(g) > 0 {
					raw = append(raw, string(g))
				}
			}
		}
	}
	out := raw[:0]
	for _, p := range raw {
		if p == "" || strings.HasPrefix(p, ".") || strings.HasPrefix(p, "/") || strings.HasPrefix(p, "~") || strings.HasPrefix(p, "@/") {
			continue
		}
		if language == "php" {
			// Namespaces read like paths: Aws\S3\S3Client is aws/s3/s3client.
			p = strings.ReplaceAll(strings.Trim(p, `\`), `\`, "/")
		}
		// Java and Kotlin wildcard imports ("a.b.*") leave a trailing dot.
		out = append(out, strings.ToLower(strings.TrimRight(p, ".")))
	}
	return out
}

// DesignOf returns each design area and library the imports touch.
func DesignOf(imports []string) map[string]map[string]bool {
	out := map[string]map[string]bool{}
	for _, imp := range imports {
		for _, d := range designLibraries {
			for _, pre := range d.prefixes {
				if importMatches(imp, pre) {
					if out[d.area] == nil {
						out[d.area] = map[string]bool{}
					}
					out[d.area][d.library] = true
					break
				}
			}
		}
	}
	return out
}

func importMatches(imp, pre string) bool {
	switch pre[len(pre)-1] {
	case '/', '.', '_', ':', '-':
		return strings.HasPrefix(imp, pre)
	}
	if imp == pre {
		return true
	}
	rest, ok := strings.CutPrefix(imp, pre)
	if !ok {
		return false
	}
	// A bare name never matches a domain: "github" is PyGithub, not
	// "github.com/...".
	if rest[0] == '.' && strings.Contains(imp, "/") && !strings.ContainsAny(pre, "./") {
		return false
	}
	return rest[0] == '/' || rest[0] == '.' || strings.HasPrefix(rest, "::")
}

// designUsage tallies design areas over files with owned lines. A file counts
// once per area, and once per library within it. It also counts the files that
// import each platform's SDKs into platforms. when gives a commit's time; it
// is nil without Git.
func designUsage(files []ownedFile, platforms map[string]PlatformUse, when func(commit string) time.Time) map[string]DesignUse {
	out := map[string]DesignUse{}
	for _, f := range files {
		if f.analyzer == nil || len(f.owned) == 0 {
			continue
		}
		imports := Imports(f.analyzer.Language(), f.file)
		for _, platform := range PlatformsOf(imports) {
			addPlatform(platforms, platform, 1, 0)
		}
		found := DesignOf(imports)
		for area, libs := range designInContent(f.file.Content) {
			if found[area] == nil {
				found[area] = map[string]bool{}
			}
			for lib := range libs {
				found[area][lib] = true
			}
		}
		if len(found) == 0 {
			continue
		}
		months := Months{}
		if when != nil {
			for _, commit := range f.owned {
				months.Add(when(commit))
			}
		}
		for area, libs := range found {
			u := out[area]
			if u.Libraries == nil {
				u.Libraries, u.Languages, u.Months = map[string]int{}, map[string]int{}, Months{}
				u.Repos = 1
			}
			u.Files++
			for lib := range libs {
				u.Libraries[lib]++
			}
			u.Languages[f.analyzer.Language()] += len(f.owned)
			u.Months.Union(months)
			out[area] = u
		}
	}
	return out
}
