package rules

// PowerShell rules follow PSScriptAnalyzer's rules.
func init() {
	ps := []string{"powershell"}
	Register(
		Rule{ID: "powershell.invoke-expression", Category: Security, Languages: ps,
			Title: "Invoke-Expression",
			Why:   "Invoke-Expression (iex) runs a string as code, so any part of it that comes from input or a download can inject commands (PSAvoidUsingInvokeExpression).",
			Focus: "Call the command directly, use the call operator & with arguments, or splat parameters."},
		Rule{ID: "powershell.empty-catch", Category: CleanCode, Languages: ps,
			Title: "Empty catch block",
			Why:   "An empty catch silently swallows the error, so failures turn into wrong behavior that is hard to trace (PSAvoidUsingEmptyCatchBlock).",
			Focus: "Handle, log, or rethrow the error; if ignoring it is intended, say why in a comment."},
		Rule{ID: "powershell.cmdlet-alias", Category: CleanCode, Languages: ps,
			Title: "Alias instead of the cmdlet name in a script",
			Why:   "Aliases such as gci, %, and ? are hard to read, and some differ between Windows PowerShell and PowerShell 7 or map to native tools on Linux (PSAvoidUsingCmdletAliases).",
			Focus: "Write the full name: Get-ChildItem, ForEach-Object, Where-Object."},
		Rule{ID: "powershell.plaintext-secure-string", Category: Security, Languages: ps,
			Title: "SecureString built from a plain-text literal",
			Why:   "ConvertTo-SecureString -AsPlainText with a literal keeps the secret in the script, readable by anyone with the file (PSAvoidUsingConvertToSecureStringWithPlainText).",
			Focus: "Read the secret with Get-Credential, Read-Host -AsSecureString, or a secret store such as Microsoft.PowerShell.SecretManagement."},
		Rule{ID: "powershell.skip-certificate-check", Category: Security, Languages: ps,
			Title: "TLS certificate validation turned off",
			Why:   "-SkipCertificateCheck or a ServerCertificateValidationCallback that returns $true accepts any certificate, so traffic can be intercepted.",
			Focus: "Trust the server's certificate properly, such as by installing its CA; skip checks only against local test servers."},
		Rule{ID: "powershell.global-variable", Category: CleanCode, Languages: ps,
			Title: "Global variable assigned",
			Why:   "$global: variables outlive the script and leak state between scripts and sessions (PSAvoidGlobalVars).",
			Focus: "Pass values as parameters and return them, or use $script: scope."},

		idiom("powershell.idiom.advanced-function", "Advanced functions", "Writes functions with [CmdletBinding()], so they get common parameters such as -Verbose and -ErrorAction.", "powershell"),
		idiom("powershell.idiom.parameter-validation", "Parameter validation", "Declares mandatory parameters and validation such as [ValidateSet()] and [ValidateNotNullOrEmpty()].", "powershell"),
		idiom("powershell.idiom.pipeline-input", "Pipeline input", "Accepts input from the pipeline with ValueFromPipeline, so functions compose like cmdlets.", "powershell"),
		idiom("powershell.idiom.should-process", "-WhatIf and -Confirm support", "Supports -WhatIf and -Confirm for changes with SupportsShouldProcess.", "powershell"),
		idiom("powershell.idiom.splatting", "Splatting", "Passes parameters as a hashtable with @params, keeping long calls readable.", "powershell"),
		idiom("powershell.idiom.comment-based-help", "Comment-based help", "Documents scripts and functions with .SYNOPSIS help that Get-Help shows.", "powershell"),
		idiom("powershell.idiom.strict-mode", "Strict mode", "Stops on errors and undefined variables with Set-StrictMode or $ErrorActionPreference = 'Stop'.", "powershell"),
		idiom("powershell.idiom.typed-parameters", "Typed parameters", "Declares parameter types such as [string] and [int], so wrong input fails early.", "powershell"),
	)
}
