// Request samples for the API reference: one template per language, filled from an operation of the OpenAPI
// document. Every sample sends the secret as `Authorization: Bearer <secret>` (the local token in local mode,
// an API key in network mode) and reads it from the environment variable VUKEM_VIDEO_KEY. Node strips the types.

export type SampleRequest = {
	method: string;
	/** The URL without the query string, with placeholders for the path parameters. */
	url: string;
	query: { name: string; value: string }[];
	/** The JSON body, when the operation takes one. */
	body?: unknown;
	/** Whether a successful answer is a JSON document (else the sample reports the status and the size). */
	json: boolean;
};

export type Sample = { label: string; lang: string; code: string };

export const SECRET_VARIABLE = "VUKEM_VIDEO_KEY";

const fullUrl = (r: SampleRequest): string => {
	if (r.query.length === 0) return r.url;
	return `${r.url}?${r.query.map((q) => `${encodeURIComponent(q.name)}=${encodeURIComponent(q.value)}`).join("&")}`;
};

const pretty = (value: unknown): string => JSON.stringify(value, null, 2);

const hasBody = (r: SampleRequest): boolean => r.body !== undefined;

const indent = (text: string, prefix: string): string =>
	text
		.split("\n")
		.map((l) => (l === "" ? l : prefix + l))
		.join("\n");

/** A JSON value as a Python literal. */
function py(value: unknown, level = 0): string {
	const pad = "    ".repeat(level + 1);
	const end = "    ".repeat(level);
	if (value === null) return "None";
	if (value === true) return "True";
	if (value === false) return "False";
	if (typeof value === "number") return String(value);
	if (typeof value === "string") return JSON.stringify(value);
	if (Array.isArray(value)) {
		if (value.length === 0) return "[]";
		return `[\n${value.map((v) => `${pad}${py(v, level + 1)},`).join("\n")}\n${end}]`;
	}
	const entries = Object.entries(value as Record<string, unknown>);
	if (entries.length === 0) return "{}";
	return `{\n${entries.map(([k, v]) => `${pad}${JSON.stringify(k)}: ${py(v, level + 1)},`).join("\n")}\n${end}}`;
}

export function curl(r: SampleRequest): Sample {
	const lines = [`curl -X ${r.method} "${fullUrl(r)}"`, `  -H "Authorization: Bearer $${SECRET_VARIABLE}"`];
	if (hasBody(r)) {
		lines.push(`  -H "Content-Type: application/json"`);
		lines.push(`  -d '${pretty(r.body).replace(/'/g, `'\\''`)}'`);
	}
	return { label: "curl", lang: "bash", code: lines.join(" \\\n") };
}

export function python(r: SampleRequest): Sample {
	const args = [`    "${r.url}",`, `    headers={"Authorization": f"Bearer {os.environ['${SECRET_VARIABLE}']}"},`];
	if (r.query.length) args.push(`    params={${r.query.map((q) => `${JSON.stringify(q.name)}: ${JSON.stringify(q.value)}`).join(", ")}},`);
	if (hasBody(r)) args.push(`    json=${py(r.body, 1)},`);
	args.push("    timeout=30,");
	const tail = r.json ? "print(response.json())" : "print(response.status_code, len(response.content))";
	const code = `import os\n\nimport requests\n\nresponse = requests.${r.method.toLowerCase()}(\n${args.join("\n")}\n)\nresponse.raise_for_status()\n${tail}`;
	return { label: "Python", lang: "python", code };
}

export function javascript(r: SampleRequest): Sample {
	const headers = [`    Authorization: \`Bearer \${process.env.${SECRET_VARIABLE}}\`,`];
	if (hasBody(r)) headers.push(`    "Content-Type": "application/json",`);
	const options = [`  method: "${r.method}",`, `  headers: {\n${headers.join("\n")}\n  },`];
	if (hasBody(r)) options.push(`  body: JSON.stringify(${indent(pretty(r.body), "  ").trimStart()}),`);
	const tail = r.json
		? "console.log(await response.json());"
		: "console.log(response.status, (await response.arrayBuffer()).byteLength);";
	const call = `const response = await fetch("${fullUrl(r)}", {`;
	const head = call.length <= 100 ? call : `const url = "${fullUrl(r)}";\nconst response = await fetch(url, {`;
	const code = `${head}\n${options.join("\n")}\n});\nif (!response.ok) throw new Error(\`\${response.status} \${await response.text()}\`);\n${tail}`;
	return { label: "JavaScript", lang: "javascript", code };
}

function goNewRequest(r: SampleRequest, body: string): string[] {
	const one = `\treq, err := http.NewRequest("${r.method}", "${fullUrl(r)}", ${body})`;
	if (one.replace(/\t/g, "    ").length <= 100) return [one];
	return [`\turl := "${fullUrl(r)}"`, `\treq, err := http.NewRequest("${r.method}", url, ${body})`];
}

export function go(r: SampleRequest): Sample {
	const imports = [...(hasBody(r) ? ['"strings"'] : []), '"fmt"', '"io"', '"net/http"', '"os"'].sort((a, b) => a.localeCompare(b));
	const body = hasBody(r) ? "body" : "nil";
	const lines = [
		"package main",
		"",
		"import (",
		...imports.map((i) => `\t${i}`),
		")",
		"",
		"func main() {",
		...(hasBody(r) ? [`\tbody := strings.NewReader(\`${pretty(r.body)}\`)`] : []),
		...goNewRequest(r, body),
		"\tif err != nil {",
		"\t\tpanic(err)",
		"\t}",
		`\treq.Header.Set("Authorization", "Bearer "+os.Getenv("${SECRET_VARIABLE}"))`,
		...(hasBody(r) ? [`\treq.Header.Set("Content-Type", "application/json")`] : []),
		"",
		"\tres, err := http.DefaultClient.Do(req)",
		"\tif err != nil {",
		"\t\tpanic(err)",
		"\t}",
		"\tdefer res.Body.Close()",
		"",
		"\tout, err := io.ReadAll(res.Body)",
		"\tif err != nil {",
		"\t\tpanic(err)",
		"\t}",
		r.json ? '\tfmt.Println(res.Status, string(out))' : '\tfmt.Println(res.Status, len(out), "bytes")',
		"}",
	];
	return { label: "Go", lang: "go", code: lines.join("\n") };
}

const phpString = (text: string): string => `'${text.replace(/\\/g, "\\\\").replace(/'/g, "\\'")}'`;

export function php(r: SampleRequest): Sample {
	const options = [
		`    CURLOPT_CUSTOMREQUEST => "${r.method}",`,
		"    CURLOPT_RETURNTRANSFER => true,",
		"    CURLOPT_HTTPHEADER => [",
		`        "Authorization: Bearer " . getenv("${SECRET_VARIABLE}"),`,
		...(hasBody(r) ? [`        "Content-Type: application/json",`] : []),
		"    ],",
		...(hasBody(r) ? [`    CURLOPT_POSTFIELDS => ${phpString(pretty(r.body))},`] : []),
	];
	const lines = [
		"<?php",
		"",
		`$ch = curl_init("${fullUrl(r)}");`,
		"curl_setopt_array($ch, [",
		...options,
		"]);",
		"",
		"$response = curl_exec($ch);",
		"if ($response === false) {",
		"    throw new RuntimeException(curl_error($ch));",
		"}",
		'echo curl_getinfo($ch, CURLINFO_HTTP_CODE), " ", ' + (r.json ? "$response" : "strlen($response)") + ", PHP_EOL;",
	];
	return { label: "PHP", lang: "php", code: lines.join("\n") };
}

function javaUri(r: SampleRequest): string[] {
	const one = `            .uri(URI.create("${fullUrl(r)}"))`;
	return one.length <= 100 ? [one] : ["            .uri(URI.create(", `                "${fullUrl(r)}"))`];
}

export function java(r: SampleRequest): Sample {
	const publisher = hasBody(r)
		? `HttpRequest.BodyPublishers.ofString("""\n${indent(pretty(r.body).replace(/\\/g, "\\\\"), "                ")}\n                """)`
		: "HttpRequest.BodyPublishers.noBody()";
	const verb = !hasBody(r) && r.method === "GET" ? ".GET()" : `.method("${r.method}", ${publisher})`;
	const handler = r.json ? "HttpResponse.BodyHandlers.ofString()" : "HttpResponse.BodyHandlers.ofByteArray()";
	const result = r.json
		? "response.statusCode() + \" \" + response.body()"
		: 'response.statusCode() + " " + response.body().length + " bytes"';
	const lines = [
		"import java.net.URI;",
		"import java.net.http.HttpClient;",
		"import java.net.http.HttpRequest;",
		"import java.net.http.HttpResponse;",
		"",
		"public class Example {",
		"    public static void main(String[] args) throws Exception {",
		"        HttpRequest request = HttpRequest.newBuilder()",
		...javaUri(r),
		`            .header("Authorization", "Bearer " + System.getenv("${SECRET_VARIABLE}"))`,
		...(hasBody(r) ? [`            .header("Content-Type", "application/json")`] : []),
		`            ${verb}`,
		"            .build();",
		"",
		`        HttpResponse<${r.json ? "String" : "byte[]"}> response = HttpClient.newHttpClient()`,
		`            .send(request, ${handler});`,
		`        System.out.println(${result});`,
		"    }",
		"}",
	];
	return { label: "Java", lang: "java", code: lines.join("\n") };
}

function csharpRequest(r: SampleRequest, method: string): string[] {
	const one = `using var request = new HttpRequestMessage(HttpMethod.${method}, "${fullUrl(r)}");`;
	return one.length <= 100 ? [one] : ["using var request = new HttpRequestMessage(", `    HttpMethod.${method}, "${fullUrl(r)}");`];
}

export function csharp(r: SampleRequest): Sample {
	const method = r.method.charAt(0) + r.method.slice(1).toLowerCase();
	const lines = [
		"using System;",
		"using System.Net.Http;",
		"using System.Net.Http.Headers;",
		...(hasBody(r) ? ["using System.Text;"] : []),
		"",
		"using var client = new HttpClient();",
		...csharpRequest(r, method),
		"request.Headers.Authorization = new AuthenticationHeaderValue(",
		`    "Bearer", Environment.GetEnvironmentVariable("${SECRET_VARIABLE}"));`,
		...(hasBody(r) ? [`request.Content = new StringContent("""\n${pretty(r.body)}\n""", Encoding.UTF8, "application/json");`] : []),
		"",
		"using var response = await client.SendAsync(request);",
		r.json
			? 'Console.WriteLine($"{(int)response.StatusCode} {await response.Content.ReadAsStringAsync()}");'
			: 'var bytes = await response.Content.ReadAsByteArrayAsync();\nConsole.WriteLine($"{(int)response.StatusCode} {bytes.Length} bytes");',
	];
	return { label: "C#", lang: "csharp", code: lines.join("\n") };
}

const rubyString = (text: string): string => `'${text.replace(/\\/g, "\\\\").replace(/'/g, "\\'")}'`;

export function ruby(r: SampleRequest): Sample {
	const klass = r.method.charAt(0) + r.method.slice(1).toLowerCase();
	const lines = [
		'require "net/http"',
		'require "uri"',
		"",
		`uri = URI("${fullUrl(r)}")`,
		`request = Net::HTTP::${klass}.new(uri)`,
		`request["Authorization"] = "Bearer #{ENV.fetch('${SECRET_VARIABLE}')}"`,
		...(hasBody(r) ? [`request["Content-Type"] = "application/json"`, `request.body = ${rubyString(pretty(r.body))}`] : []),
		"",
		'response = Net::HTTP.start(uri.hostname, uri.port, use_ssl: uri.scheme == "https") do |http|',
		"  http.request(request)",
		"end",
		r.json ? 'puts "#{response.code} #{response.body}"' : 'puts "#{response.code} #{response.body.to_s.bytesize} bytes"',
	];
	return { label: "Ruby", lang: "ruby", code: lines.join("\n") };
}

/** The eight samples of an operation, in the order of the tabs. */
export function samples(r: SampleRequest): Sample[] {
	return [curl, python, javascript, go, php, java, csharp, ruby].map((f) => f(r));
}
