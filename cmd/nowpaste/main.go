package main

import (
	"context"
	"flag"
	"fmt"
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

var (
	Version = "current"
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
		minLevel           string
		pathPrefix         string
		listen             string
		token              string
		basicUser          string
		basicPass          string
		searchChannelTypes string
	)
	flag.CommandLine.Usage = func() {
		fmt.Fprintln(flag.CommandLine.Output(), "nowpaste [options]")
		fmt.Fprintln(flag.CommandLine.Output(), "version:", Version)
		flag.CommandLine.PrintDefaults()
	}
	flag.StringVar(&minLevel, "log-level", "info", "log level")
	flag.StringVar(&pathPrefix, "path-prefix", "/", "endpoint path prefix")
	flag.StringVar(&listen, "listen", ":8080", "http server run on")
	flag.StringVar(&token, "slack-token", "", "slack token")
	flag.StringVar(&basicUser, "basic-user", "", "basic auth user")
	flag.StringVar(&basicPass, "basic-pass", "", "basic auth pass")
	flag.StringVar(&searchChannelTypes, "search-channel-types", "", "search channel types. comma separated enums (public_channel,private_channel,mpim,im)")
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if ssmPath := os.Getenv("NOWPASTE_SSM_PATH"); ssmPath != "" {
		flag.VisitAll(SSMParameterPathToFlag(ctx, ssmPath, "NOWPASTE_"))
	}
	if ssmNames := os.Getenv("NOWPASTE_SSM_NAMES"); ssmNames != "" {
		flag.VisitAll(SSMParameterNamesToFlag(ctx, ssmNames, "NOWPASTE_"))
	}
	flag.VisitAll(flagx.EnvToFlagWithPrefix("NOWPASTE_"))
	flag.Parse()
	filter.SetMinLevel(logutils.LogLevel(strings.ToLower(minLevel)))
	log.Println("[debug] log level:", minLevel)
	if token == "" {
		log.Fatalln("[error] slack-token is required")
	}
	app := nowpaste.New(token)
	if basicUser != "" && basicPass != "" {
		app.SetBasicAuth(basicUser, basicPass)
	}
	if searchChannelTypes != "" {
		app.SetSearchChannelTypes(strings.Split(searchChannelTypes, ","))
	}
	ridge.RunWithContext(ctx, listen, pathPrefix, app)
}

func SSMParameterPathToFlag(ctx context.Context, ssmPath string, prefix string) func(*flag.Flag) {
	client, err := newSSMClient(ctx)
	if err != nil {
		log.Printf("[warn] ssm parameter path to flag: %s", err.Error())
		return func(_ *flag.Flag) {}
	}
	log.Printf("[info] Get SSM Parameter by path: %s", ssmPath)
	p := ssm.NewGetParametersByPathPaginator(client, &ssm.GetParametersByPathInput{
		Path:           aws.String(ssmPath),
		WithDecryption: aws.Bool(true),
		Recursive:      aws.Bool(true),
	})
	values := make(map[string]string)
	for p.HasMorePages() {
		output, err := p.NextPage(ctx)
		if err != nil {
			log.Printf("[warn] ssm parameter path to flag: %s", err.Error())
			return func(_ *flag.Flag) {}
		}
		for _, param := range output.Parameters {
			log.Printf("[debug] Get SSM Parameter: %s", *param.Name)
			values[*param.Name] = *param.Value
		}
	}
	log.Printf("[info] Get %d SSM Parameters by path", len(values))
	return newLookupFunc(values, prefix)
}

func SSMParameterNamesToFlag(ctx context.Context, names string, prefix string) func(*flag.Flag) {
	client, err := newSSMClient(ctx)
	if err != nil {
		log.Printf("[warn] ssm parameter names to flag: new ssm client:%s", err.Error())
		return func(_ *flag.Flag) {}
	}
	parameterNames := strings.Split(names, ",")
	values := make(map[string]string, len(parameterNames))
	for _, parameterName := range parameterNames {
		log.Printf("[debug] Get SSM Parameter: %s", parameterName)
		output, err := client.GetParameter(ctx, &ssm.GetParameterInput{
			Name:           aws.String(strings.TrimSpace(strings.Trim(parameterName, ","))),
			WithDecryption: aws.Bool(true),
		})
		if err != nil {
			log.Printf("[warn] ssm parameter names to flag: get parameter: %s", err.Error())
			return func(_ *flag.Flag) {}
		}
		log.Printf("[debug] Get SSM Parameter: %s", *output.Parameter.Name)
		values[*output.Parameter.Name] = *output.Parameter.Value
	}
	log.Printf("[debug] Get %d SSM Parameters by names", len(values))
	return newLookupFunc(values, prefix)
}

func newSSMClient(ctx context.Context) (*ssm.Client, error) {
	opts := make([]func(*config.LoadOptions) error, 0)
	if region := os.Getenv("AWS_DEFAULT_REGION"); region != "" {
		opts = append(opts, config.WithRegion(region))
	}
	awsCfg, err := config.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return nil, err
	}
	var ssmOpts []func(*ssm.Options)
	if endpoint := os.Getenv("AWS_ENDPOINT"); endpoint != "" {
		ssmOpts = append(ssmOpts, func(o *ssm.Options) {
			o.BaseEndpoint = aws.String(endpoint)
		})
	}
	client := ssm.NewFromConfig(awsCfg, ssmOpts...)
	return client, nil
}

func newLookupFunc(values map[string]string, prefix string) func(*flag.Flag) {
	return func(f *flag.Flag) {
		names := []string{
			strings.ToUpper(prefix + strings.ReplaceAll(f.Name, "-", "_")),
			strings.ToUpper(strings.ReplaceAll(f.Name, "-", "_")),
			strings.ToLower(prefix + strings.ReplaceAll(f.Name, "-", "_")),
			strings.ToLower(strings.ReplaceAll(f.Name, "-", "_")),
		}
		for _, name := range names {
			for paramPath, value := range values {
				if strings.HasSuffix(paramPath, name) {
					log.Printf("[info] use SSM Parameter: %s", paramPath)
					f.Value.Set(value)
					return
				}
			}
		}
	}
}
