package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	"github.com/fatih/color"
	"github.com/fujiwara/logutils"
	"github.com/fujiwara/ridge"
	flagx "github.com/ken39arg/go-flagx"
	"github.com/mashiike/nowpaste"
)

func main() {
	filter := &logutils.LevelFilter{
		Levels: []logutils.LogLevel{"debug", "info", "notice", "warn", "error"},
		ModifierFuncs: []logutils.ModifierFunc{
			logutils.Color(color.FgHiBlack),
			nil,
			logutils.Color(color.FgHiBlue),
			logutils.Color(color.FgYellow),
			logutils.Color(color.FgRed, color.BgBlack),
		},
		MinLevel: "info",
		Writer:   os.Stderr,
	}
	log.SetOutput(filter)
	var (
		minLevel   string
		pathPrefix string
		listen     string
		token      string
	)
	flag.StringVar(&minLevel, "log-level", "info", "log level")
	flag.StringVar(&pathPrefix, "path-prefix", "/", "endpoint path prefix")
	flag.StringVar(&listen, "listen", ":8080", "http server run on")
	flag.StringVar(&token, "slack-token", "", "slack token")
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if ssmPath := os.Getenv("NOWPASTE_SSM_PATH"); ssmPath != "" {
		flag.VisitAll(SSMParameterToFlag(ctx, ssmPath, "NOWPASTE_"))
	}
	flag.VisitAll(flagx.EnvToFlagWithPrefix("NOWPASTE_"))
	flag.Parse()
	filter.SetMinLevel(logutils.LogLevel(strings.ToLower(minLevel)))
	log.Println("[debug] log level:", minLevel)
	if token == "" {
		log.Fatalln("[error] slack-token is required")
	}
	mux := nowpaste.New(token)
	ridge.RunWithContext(ctx, listen, pathPrefix, mux)
}

func SSMParameterToFlag(ctx context.Context, ssmPath string, prefix string) func(*flag.Flag) {
	opts := make([]func(*config.LoadOptions) error, 0)
	if region := os.Getenv("AWS_DEFAULT_REGION"); region != "" {
		opts = append(opts, config.WithRegion(region))
	}
	awsCfg, err := config.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		log.Printf("[warn] ssm parameter to flag: %s", err.Error())
		return func(_ *flag.Flag) {}
	}
	ssmOpts := make([]func(*ssm.Options), 0)
	if endpoint := os.Getenv("SSM_ENDPOINT"); endpoint != "" {
		ssm.WithEndpointResolver(ssm.EndpointResolverFunc(func(region string, options ssm.EndpointResolverOptions) (aws.Endpoint, error) {
			return aws.Endpoint{
				URL:           endpoint,
				SigningRegion: region,
				PartitionID:   "aws",
			}, nil
		}))
	}
	client := ssm.NewFromConfig(awsCfg, ssmOpts...)
	p := ssm.NewGetParametersByPathPaginator(client, &ssm.GetParametersByPathInput{
		Path:           aws.String(ssmPath),
		WithDecryption: *aws.Bool(true),
		Recursive:      *aws.Bool(true),
	})
	values := make(map[string]string)
	for p.HasMorePages() {
		output, err := p.NextPage(ctx)
		if err != nil {
			log.Printf("[warn] ssm parameter to flag: %s", err.Error())
			return func(_ *flag.Flag) {}
		}
		for _, param := range output.Parameters {
			values[*param.Name] = *param.Value
		}
	}
	return func(f *flag.Flag) {
		names := []string{
			strings.ToUpper(prefix + strings.ReplaceAll(f.Name, "-", "_")),
			strings.ToUpper(strings.ReplaceAll(f.Name, "-", "_")),
			strings.ToLower(prefix + strings.ReplaceAll(f.Name, "-", "_")),
			strings.ToLower(strings.ReplaceAll(f.Name, "-", "_")),
		}
		for _, name := range names {
			if s, ok := values[name]; ok {
				f.Value.Set(s)
				return
			}
		}
	}
}
