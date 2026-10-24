package facts

// The built-in sensitive areas. A change that lands in one is shown to
// reviewers as context; areas are not classes and never score.
const (
	AreaAuth           = "auth"
	AreaInfrastructure = "infrastructure"
	AreaMigrations     = "migrations"
	AreaSecrets        = "secrets"
)

// builtinAreas is the one listing of riskelle's built-in sensitive-area
// patterns, beside builtin's path classes and in the same glob syntax.
// Repo-added areas (from .risk/rules.json) extend it; they never replace it.
// The entries are sorted by name, so Areas' result is sorted.
var builtinAreas = []struct {
	name     string
	patterns []string
}{
	{AreaAuth, []string{
		"**/auth/**", "**/oauth/**", "**/*Auth*/**", "**/*Auth*.cs",
		"**/auth.*", "**/auth_*", "**/*_auth.*",
		"**/identity/**", "**/*Identity*/**",
		"**/jwt*", "**/*Jwt*", "**/*Token*.cs",
	}},
	{AreaInfrastructure, []string{
		// infrastructure as code
		"**/*.tf", "**/*.tfvars", "**/*.bicep", "**/*.bicepparam", "**/azuredeploy*.json",
		"**/terraform/**", "**/infra/**", "**/infrastructure/**",
		"**/k8s/**", "**/kubernetes/**", "**/helm/**",
		// containers
		"**/Dockerfile", "**/Dockerfile.*", "**/*.dockerfile",
		"**/docker-compose*.yml", "**/docker-compose*.yaml",
		// CI and delivery pipelines
		".github/workflows/**", "azure-pipelines*.yml", "azure-pipelines*.yaml", ".azuredevops/**",
	}},
	{AreaMigrations, []string{
		"**/migrations/**", "**/Migrations/**", "**/migrate/**", "**/*.sql",
	}},
	{AreaSecrets, []string{
		"**/*secret*", "**/*Secret*", "**/*secret*/**", "**/*Secret*/**",
		"**/*.pem", "**/*.key", "**/*.pfx", "**/*.p12", "**/*.jks", "**/*.keystore",
		"**/id_rsa*", "**/id_ed25519*", "**/.env", "**/.env.*",
		"**/crypto/**", "**/*Crypto*", "**/*Encrypt*",
	}},
}

type compiledArea struct {
	name  string
	globs Globs
}

// compiledAreas holds builtinAreas compiled once.
var compiledAreas = func() []compiledArea {
	out := make([]compiledArea, 0, len(builtinAreas))
	for _, a := range builtinAreas {
		out = append(out, compiledArea{a.name, CompileGlobs(a.patterns)})
	}
	return out
}()

// Areas returns the built-in sensitive areas a slash-separated, repo-relative
// path falls in, sorted and each at most once; nil when none. It reads only
// the path, so it serves planned paths too.
func Areas(p string) []string {
	var names []string
	for _, a := range compiledAreas {
		if a.globs.Match(p) {
			names = append(names, a.name)
		}
	}
	return names
}
