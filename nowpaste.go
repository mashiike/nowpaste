package nowpaste

import (
	"bytes"
	"context"
	crand "crypto/rand"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/arn"
	"github.com/aws/aws-sdk-go-v2/service/sns"
	"github.com/gorilla/mux"
	"github.com/slack-go/slack"
)

type NowPaste struct {
	router    *mux.Router
	client    *slack.Client
	basicUser *string
	basicPass *string
}

func New(slackToken string) *NowPaste {
	return newWithClient(slack.New(slackToken))
}

func newWithClient(client *slack.Client) *NowPaste {
	nwp := &NowPaste{
		router: mux.NewRouter(),
		client: client,
	}
	nwp.setRoute()
	return nwp
}

func (nwp *NowPaste) SetBasicAuth(user string, pass string) {
	nwp.basicUser = &user
	nwp.basicPass = &pass
}

func (nwp *NowPaste) setRoute() {
	nwp.router.HandleFunc("/", nwp.postDefault).Methods(http.MethodPost)
	nwp.router.HandleFunc("/amazon-sns/{channel}", nwp.postSNS).Methods(http.MethodPost)
}

func (nwp *NowPaste) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	log.Printf("[notice] %s %s", req.Method, req.URL.String())
	if nwp.basicUser != nil && nwp.basicPass != nil {
		if !nwp.CheckBasicAuth(req) {
			w.Header().Add("WWW-Authenticate", `Basic realm="SECRET AREA"`)
			http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
			return
		}
	}
	nwp.router.ServeHTTP(w, req)
}

func (nwp *NowPaste) CheckBasicAuth(req *http.Request) bool {
	clientID, clientSecret, ok := req.BasicAuth()
	if !ok {
		return false
	}
	return clientID == *nwp.basicUser && clientSecret == *nwp.basicPass
}

func (nwp *NowPaste) newContent(req *http.Request) *Content {
	content := &Content{}
	if asFile := req.URL.Query().Get("as_file"); asFile != "" {
		b, err := strconv.ParseBool(asFile)
		if err == nil {
			content.AsFile = b
		} else {
			log.Printf("[warn] as_file query param parse failed: %s", err.Error())
		}
	}
	if asMessage := req.URL.Query().Get("as_message"); asMessage != "" {
		b, err := strconv.ParseBool(asMessage)
		if err == nil {
			content.AsMessage = b
		} else {
			log.Printf("[warn] as_message query param parse failed: %s", err.Error())
		}
	}
	return content
}

func (nwp *NowPaste) postDefault(w http.ResponseWriter, req *http.Request) {
	defer req.Body.Close()
	content := nwp.newContent(req)
	contentType := req.Header.Get("Content-Type")
	log.Printf("[debug] Content-Type: %s", contentType)
	switch strings.ToLower(contentType) {
	case "application/x-www-form-urlencoded":
		username := req.FormValue("username")
		if username == "" {
			username = "nowpaste"
		}
		escapeTextStr := req.FormValue("escape_text")
		var escapeText bool
		if escapeTextStr != "" {
			if b, err := strconv.ParseBool(escapeTextStr); err == nil {
				escapeText = b
			}
		}
		codeBlockTextStr := req.FormValue("code_block_text")
		var codeBlockText bool
		if codeBlockTextStr != "" {
			if b, err := strconv.ParseBool(codeBlockTextStr); err == nil {
				codeBlockText = b
			}
		}
		content.Merge(&Content{
			Channel:       req.FormValue("channel"),
			Text:          req.FormValue("text"),
			Username:      username,
			IconEmoji:     req.FormValue("icon_emoji"),
			IconURL:       req.FormValue("icon_url"),
			EscapeText:    escapeText,
			CodeBlockText: codeBlockText,
		})
	case "application/json":
		var buf bytes.Buffer
		decoder := json.NewDecoder(io.TeeReader(req.Body, &buf))
		if err := decoder.Decode(content); err != nil {
			log.Printf("[info] can not read as json: %s", err.Error())
			http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
			return
		}
		if !content.IsRich() {
			content.Text = buf.String()
			content.CodeBlockText = true
			if content.Channel == "" {
				content.Channel = req.URL.Query().Get("channel")
			}
			if content.Username == "" {
				content.Username = req.URL.Query().Get("username")
				if content.Username == "" {
					content.Username = "nowpaste"
				}
			}
		}
	default:
		channel := req.URL.Query().Get("channel")
		if channel == "" {
			http.Error(w, "query param `channel` is required", http.StatusBadRequest)
			return
		}
		username := req.URL.Query().Get("username")
		if username == "" {
			username = "nowpaste"
		}
		escapeTextStr := req.URL.Query().Get("escape_text")
		var escapeText bool
		if escapeTextStr != "" {
			if b, err := strconv.ParseBool(escapeTextStr); err == nil {
				escapeText = b
			}
		}
		codeBlockTextStr := req.URL.Query().Get("code_block_text")
		var codeBlockText bool
		if codeBlockTextStr != "" {
			if b, err := strconv.ParseBool(codeBlockTextStr); err == nil {
				codeBlockText = b
			}
		}
		bs, err := io.ReadAll(req.Body)
		if err != nil {
			log.Printf("[info] can not read body: %s", err.Error())
			http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
			return
		}
		content.Merge(&Content{
			Channel:       channel,
			Text:          string(bs),
			Username:      username,
			EscapeText:    escapeText,
			CodeBlockText: codeBlockText,
			IconEmoji:     req.URL.Query().Get("icon_emoji"),
			IconURL:       req.URL.Query().Get("icon_url"),
		})
	}
	if err := nwp.postContent(req.Context(), content); err != nil {
		var rle *slack.RateLimitedError
		if errors.As(err, &rle) {
			log.Printf("[warn] rate limit: %s", err.Error())
			w.Header().Add("Retry-After", rle.RetryAfter.String())
			http.Error(w, http.StatusText(http.StatusTooManyRequests), http.StatusTooManyRequests)
			return
		}
		log.Printf("[error] post failed: %s", err.Error())
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
	io.WriteString(w, http.StatusText(http.StatusOK))
}

// https://docs.aws.amazon.com/sns/latest/dg/json-formats.html
type HTTPNotification struct {
	Type             string    `json:"Type"`
	MessageId        string    `json:"MessageId"`
	Token            string    `json:"Token,omitempty"` // Only for subscribe and unsubscribe
	TopicArn         string    `json:"TopicArn"`
	Subject          string    `json:"Subject,omitempty"` // Only for Notification
	Message          string    `json:"Message"`
	SubscribeURL     string    `json:"SubscribeURL,omitempty"` // Only for subscribe and unsubscribe
	Timestamp        time.Time `json:"Timestamp"`
	SignatureVersion string    `json:"SignatureVersion"`
	Signature        string    `json:"Signature"`
	SigningCertURL   string    `json:"SigningCertURL"`
	UnsubscribeURL   string    `json:"UnsubscribeURL,omitempty"` // Only for notifications
}

func (nwp *NowPaste) postSNS(w http.ResponseWriter, req *http.Request) {
	defer req.Body.Close()
	var n HTTPNotification
	decoder := json.NewDecoder(req.Body)
	if err := decoder.Decode(&n); err != nil {
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}
	log.Println("[info] sns", n.Type, n.TopicArn, n.Subject)
	vars := mux.Vars(req)
	channel, ok := vars["channel"]
	if !ok || channel == "" {
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}
	content := nwp.newContent(req)
	switch n.Type {
	case "SubscriptionConfirmation":
		arnObj, err := arn.Parse(n.TopicArn)
		if err != nil {
			log.Printf("[error] topic ARN `%s` parse failed: %s", n.TopicArn, err.Error())
			http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
			return
		}
		client := sns.New(sns.Options{Region: arnObj.Region})
		_, err = client.ConfirmSubscription(req.Context(), &sns.ConfirmSubscriptionInput{
			Token:                     aws.String(n.Token),
			TopicArn:                  aws.String(n.TopicArn),
			AuthenticateOnUnsubscribe: aws.String("no"),
		})
		if err != nil {
			log.Println("[warn]", err)
			break
		}
		var out bytes.Buffer
		fmt.Fprintf(&out, "this message posted by nowpaste\n\n")
		fmt.Fprintf(&out, "%s from %s\n", n.Type, n.TopicArn)
		if subscriptionArn := req.Header.Get("x-amz-sns-subscription-arn"); subscriptionArn != "" {
			fmt.Fprintf(&out, "Subscribe by %s\n", subscriptionArn)
		}

		fmt.Fprintf(&out, "You have chosen to subscribe to the topic %s.\n", n.TopicArn)
		fmt.Fprintln(&out, "This Subscription was automatically confirmed by nowpaste.")
		content.CodeBlockText = true
		content.Text = out.String()
	case "Notification":
		log.Printf("[notice] %s from %s, subject=%s", n.Type, n.TopicArn, n.Subject)
		decoder := json.NewDecoder(strings.NewReader(n.Message))
		if err := decoder.Decode(&content); err != nil {
			content.Text = strings.Trim(string(n.Message), "\"")
		}
		if !content.IsRich() {
			content.Text = strings.Trim(string(n.Message), "\"")
		}
		if content.Text != "" {
			escapeTextStr := req.URL.Query().Get("escape_text")
			if escapeTextStr != "" {
				if b, err := strconv.ParseBool(escapeTextStr); err == nil {
					content.EscapeText = b
				}
			}
			codeBlockTextStr := req.URL.Query().Get("code_block_text")
			if codeBlockTextStr != "" {
				if b, err := strconv.ParseBool(codeBlockTextStr); err == nil {
					content.CodeBlockText = b
				}
			}
		}
	}
	content.Channel = channel
	if content.IconEmoji == "" && content.IconURL == "" {
		content.IconEmoji = req.URL.Query().Get("icon_emoji")
		content.IconURL = req.URL.Query().Get("icon_url")
	}
	if content.Username == "" {
		content.Username = req.URL.Query().Get("username")
	}
	if err := nwp.postContent(req.Context(), content); err != nil {
		var rle *slack.RateLimitedError
		if errors.As(err, &rle) {
			log.Printf("[warn] rate limit: %s", err.Error())
			w.Header().Add("Retry-After", rle.RetryAfter.String())
			http.Error(w, http.StatusText(http.StatusTooManyRequests), http.StatusTooManyRequests)
			return
		}
		log.Printf("[warn] %s post failed: %s", n.TopicArn, err.Error())
	}
	w.WriteHeader(http.StatusOK)
	io.WriteString(w, http.StatusText(http.StatusOK))
}

type Content struct {
	Channel       string             `json:"channel,omitempty"`
	IconEmoji     string             `json:"icon_emoji,omitempty"`
	IconURL       string             `json:"icon_url,omitempty"`
	Username      string             `json:"username"`
	Blocks        json.RawMessage    `json:"blocks,omitempty"`
	Text          string             `json:"text,omitempty"`
	EscapeText    bool               `json:"escape_text,omitempty"`
	CodeBlockText bool               `json:"code_block_text,omitempty"`
	Attachments   []slack.Attachment `json:"attachments,omitempty"`
	AsFile        bool               `json:"as_file,omitempty"`
	AsMessage     bool               `json:"as_message,omitempty"`
}

func (content *Content) IsRich() bool {
	return len(content.Blocks) > 0 || len(content.Attachments) > 0 || content.Text != ""
}

func (content *Content) Merge(c *Content) {
	if c.Channel != "" {
		content.Channel = c.Channel
	}
	if c.IconEmoji != "" {
		content.IconEmoji = c.IconEmoji
	}
	if c.IconURL != "" {
		content.IconURL = c.IconURL
	}
	if c.Username != "" {
		content.Username = c.Username
	}
	if len(c.Blocks) > 0 {
		content.Blocks = c.Blocks
	}
	if c.Text != "" {
		content.Text = c.Text
	}
	if c.EscapeText {
		content.EscapeText = c.EscapeText
	}
	if c.CodeBlockText {
		content.CodeBlockText = c.CodeBlockText
	}
	if len(c.Attachments) > 0 {
		content.Attachments = c.Attachments
	}
	if c.AsFile {
		content.AsFile = c.AsFile
	}
	if c.AsMessage {
		content.AsMessage = c.AsMessage
	}
}

var apiRetrier = &retrier{
	timeout: 10 * time.Second,
	jitter:  500 * time.Millisecond,
}

// see also https://api.slack.com/methods/chat.postMessage#:~:text=For%20best%20results%2C%20limit%20the,consider%20uploading%20a%20snippet%20instead.
const uploadFilesThreshold = 4000
const textMaxLength = 40000
const linesThreshold = 6

const postAsMessage = "message"
const postAsFile = "file"

func (nwp *NowPaste) detectPostMode(content *Content) string {
	if content.AsMessage {
		return postAsMessage
	}
	if content.AsFile {
		return postAsFile
	}
	textSize := len(content.Text)
	textLines := strings.Count(content.Text, "\n") + 1
	log.Printf("[debug] content.Text: textSize=%d textLines=%d", textSize, textLines)
	if textSize >= uploadFilesThreshold {
		return postAsFile
	}
	if textLines >= linesThreshold && !content.CodeBlockText {
		return postAsFile
	}
	return postAsMessage
}

func (nwp *NowPaste) postContent(ctx context.Context, content *Content) error {
	if content.Channel == "" {
		return errors.New("channel is required")
	}
	switch nwp.detectPostMode(content) {
	case postAsFile:
		return nwp.postFile(ctx, content)
	case postAsMessage:
		return nwp.postMessage(ctx, content)
	default:
		return errors.New("unknown post mode")
	}
}

func (nwp *NowPaste) postFile(ctx context.Context, content *Content) error {
	var f *slack.File
	err, timeout := apiRetrier.Do(ctx, func() error {
		var err error
		f, err = nwp.client.UploadFileContext(ctx, slack.FileUploadParameters{
			Channels: []string{content.Channel},
			Content:  content.Text,
		})
		return err
	})
	if err != nil {
		if timeout {
			return err
		}
		var ser slack.SlackErrorResponse
		if !errors.As(err, &ser) {
			return fmt.Errorf("upload files: %w", err)
		}
		if ser.Err != "not_in_channel" {
			log.Printf("[debug] try upload files, slack error response: %s", ser.Error())
			return fmt.Errorf("upload files: %w", ser)
		}

		log.Printf("[warn] try upload files but not in channel, try join channel to %s", content.Channel)
		err, _ = apiRetrier.Do(ctx, func() error {
			_, _, _, err := nwp.client.JoinConversationContext(ctx, content.Channel)
			return err
		})
		if err != nil {
			log.Printf("[debug] join channel: %#v", err)
			return fmt.Errorf("join channel may be not channel id: %w", err)
		}
		err, _ = apiRetrier.Do(ctx, func() error {
			var err error
			f, err = nwp.client.UploadFileContext(ctx, slack.FileUploadParameters{
				Channels: []string{content.Channel},
				Content:  content.Text,
			})
			return err
		})
		if err != nil {
			return fmt.Errorf("retry upload files: %w", err)
		}
	}
	log.Printf("[info] upload File to %s, file id is `%s`", content.Channel, f.ID)
	return nil
}

func (nwp *NowPaste) postMessage(ctx context.Context, content *Content) error {
	opts := make([]slack.MsgOption, 0)
	if content.IconEmoji != "" {
		opts = append(opts, slack.MsgOptionIconEmoji(content.IconEmoji))
	} else if content.IconURL != "" {
		opts = append(opts, slack.MsgOptionIconURL(content.IconURL))
	}
	if content.Username != "" {
		opts = append(opts, slack.MsgOptionUsername(content.Username))
	}
	if len(content.Blocks) > 0 {
		var blocks slack.Blocks
		if err := json.Unmarshal(content.Blocks, &blocks); err != nil {
			return err
		}
		opts = append(opts, slack.MsgOptionBlocks(blocks.BlockSet...))
	}
	if len(content.Attachments) > 0 {
		opts = append(opts, slack.MsgOptionAttachments(content.Attachments...))
	}
	if content.Text != "" {
		if content.CodeBlockText {
			content.Text = "```" + content.Text + "```"
		}
		opts = append(opts, slack.MsgOptionText(content.Text, content.EscapeText))
	}
	log.Printf("[debug] try post message to %s", content.Channel)
	var postedChannelID, postedTimestamp string
	err, _ := apiRetrier.Do(ctx, func() error {
		var err error
		postedChannelID, postedTimestamp, err = nwp.client.PostMessageContext(ctx, content.Channel, opts...)
		return err
	})
	if err != nil {
		return fmt.Errorf("post message: %w", err)
	}
	if postedChannelID == content.Channel {
		log.Printf("[info] post Message to %s at %s", postedChannelID, postedTimestamp)
	} else {
		log.Printf("[info] post Message to %s(%s) at %s", content.Channel, postedChannelID, postedTimestamp)
	}
	return nil
}

type retrier struct {
	timeout time.Duration
	jitter  time.Duration
	mu      sync.Mutex
	rand    *rand.Rand
}

func (r *retrier) Do(ctx context.Context, f func() error) (error, bool) {
	start := time.Now()
	err := f()
	var t *time.Timer
	var rle *slack.RateLimitedError
	for err != nil && errors.As(err, &rle) && rle.Retryable() {
		if time.Since(start) >= r.timeout {
			return err, true
		}
		delay := rle.RetryAfter + r.randomJitter()
		if t == nil {
			t = time.NewTimer(delay)
			defer t.Stop()
		} else {
			t.Reset(delay)
		}
		select {
		case <-t.C:
		case <-ctx.Done():
			return ctx.Err(), true
		}
		err = f()
	}
	return err, time.Since(start) >= r.timeout
}

func (r *retrier) randomJitter() time.Duration {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.rand == nil {
		var seed int64
		if err := binary.Read(crand.Reader, binary.LittleEndian, &seed); err != nil {
			seed = time.Now().UnixNano()
		}
		r.rand = rand.New(rand.NewSource(seed))
	}
	return time.Duration(r.rand.Int63n(int64(r.jitter)))
}
