package tiles

import (
	"fmt"
	"strings"

	"github.com/vinodhalaharvi/tilegen/internal/compile"
)

func init() {
	compile.RegisterOffer(&compile.Offer{
		Tile:       "aws-secrets-manager",
		Engine:     "aws-secrets-manager",
		Capability: "secrets",
		Doc:        "one AWS Secrets Manager secret holding a JSON object, read through a short cache",
		Cost: compile.Cost{{Dim: "llm-work", Value: 0}, {Dim: "maintenance", Value: 2, Source: "derived", Note: "an IAM policy to keep in step with the secret, and its JSON keys with the spec"},
			{Dim: "dependency", Value: 4, Source: "derived", Note: "the AWS SDK's module tree"},
			{Dim: "runtime", Value: 1, Source: "derived", Note: "a network read when the cache expires"},
			{Dim: "operations", Value: 2, Source: "derived", Note: "nothing to run; a secret, its rotation and its IAM policy to look after"},
			{Dim: "uncertainty", Value: 2, Source: "derived", Note: "the code it writes builds against the SDK and has not been run against AWS"}},
		Satisfies: map[string]string{
			"rotating": "a read is kept for a ttl you choose, so a rotated secret is seen within it",
			"audited":  "CloudTrail records every GetSecretValue with the identity that made it",
		},
		Impl: &compile.SecretSource{Suffix: "AWSSecrets", Emit: awsSecrets,
			Import: [2]string{"secretsmanager", "github.com/aws/aws-sdk-go-v2/service/secretsmanager"}},
	})
	compile.RegisterAlias("aws-secrets-manager", "aws")
}

func awsSecrets(pkg string, secrets []compile.Secret) string {
	var b strings.Builder
	w := func(format string, a ...any) { fmt.Fprintf(&b, format, a...) }
	w("// AWSSecrets reads secrets from one AWS Secrets Manager secret, whose value\n// is a JSON object of strings with the keys:\n//\n%s//\n"+
		"// A read is kept for ttl, so a rotated secret is seen within ttl and AWS is\n// not asked on every call. If AWS cannot be reached once a read is in hand,\n"+
		"// that read keeps being served and AWS is asked again on the next call.\n// CloudTrail records each read with the identity the client runs as.\n"+
		"type AWSSecrets struct {\n\tclient *secretsmanager.Client\n\tid     string\n\tttl    time.Duration\n\n\tmu      sync.Mutex\n\tdata    map[string]string\n\tfetched time.Time\n}\n\n",
		compile.SecretList(secrets, compile.Secret.ExternalKey))
	w("// NewAWSSecrets reads the secret once and checks every key is there, so a\n// wrong name or a missing permission fails when the service starts. id is\n"+
		"// the secret's name or ARN. The client arrives configured: its region and\n// the role it runs as are decided where the client is made.\n"+
		"func NewAWSSecrets(ctx context.Context, client *secretsmanager.Client, id string, ttl time.Duration) (*AWSSecrets, error) {\n"+
		"\ts := &AWSSecrets{client: client, id: id, ttl: ttl}\n\tvar missing []string\n"+
		"\tfor _, key := range []string{%s} {\n\t\tif _, err := s.read(ctx, key); err != nil {\n\t\t\tif s.data == nil {\n\t\t\t\treturn nil, err // the secret itself could not be read\n\t\t\t}\n\t\t\tmissing = append(missing, key)\n\t\t}\n\t}\n"+
		"\tif len(missing) > 0 {\n\t\treturn nil, fmt.Errorf(\"%s: secret %%s has no %%s\", id, strings.Join(missing, \", \"))\n\t}\n\treturn s, nil\n}\n\n",
		compile.Quoted(secrets, compile.Secret.ExternalKey), pkg)
	w("func (s *AWSSecrets) read(ctx context.Context, key string) (string, error) {\n\ts.mu.Lock()\n\tdefer s.mu.Unlock()\n"+
		"\tif s.data == nil || time.Since(s.fetched) >= s.ttl {\n"+
		"\t\tvar data map[string]string\n"+
		"\t\tout, err := s.client.GetSecretValue(ctx, &secretsmanager.GetSecretValueInput{SecretId: &s.id})\n"+
		"\t\tswitch {\n\t\tcase err != nil:\n\t\tcase out.SecretString == nil:\n\t\t\terr = errors.New(\"it holds binary, not a JSON object\")\n"+
		"\t\tdefault:\n\t\t\terr = json.Unmarshal([]byte(*out.SecretString), &data)\n\t\t}\n"+
		"\t\tswitch {\n\t\tcase err == nil:\n\t\t\ts.data, s.fetched = data, time.Now()\n"+
		"\t\tcase s.data == nil:\n\t\t\treturn \"\", fmt.Errorf(\"%s: secret %%s: %%w\", s.id, err)\n\t\t}\n\t}\n"+
		"\tv := s.data[key]\n\tif v == \"\" {\n\t\treturn \"\", fmt.Errorf(\"%s: secret %%s has no %%s\", s.id, key)\n\t}\n\treturn v, nil\n}\n", pkg, pkg)
	for _, s := range secrets {
		w("\nfunc (s *AWSSecrets) %s(ctx context.Context) (string, error) { return s.read(ctx, %q) }\n", s.Name, s.ExternalKey())
	}
	return b.String()
}
