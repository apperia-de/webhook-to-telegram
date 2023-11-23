# Webhook to Telegram message

[![Go Report Card](https://goreportcard.com/badge/github.com/sknr/webhook-to-telegram)](https://goreportcard.com/report/github.com/sknr/webhook-to-telegram)
![GitHub go.mod Go version](https://img.shields.io/github/go-mod/go-version/sknr/webhook-to-telegram?style=flat)
![GitHub Licence](https://img.shields.io/github/license/sknr/webhook-to-telegram)

A Telegram bot which may handle webhook updates from several 
services like GitHub, Kofi, PayPal, etc. and sends them as Telegram messages.

## Required steps

1. Create a Telegram bot via [BotFather](https://t.me/botfather) and obtain a bot token.
2. Edit your `.env` file or your env vars directly and set both `BOT_TOKEN` and `TELEGRAM_WEBHOOK_URL` accordingly.
3. Copy or rename `config_example.yml` to `config.yml` and configure to your desired webhook service needs. (see example below)
4. Build cmd/main.go via `go build main.go -o wh2t` and run the Telegram bot server.

## Example: config.yml

In order to turn json data send via webhook to your Telegram bot into a message send to your Telegram account, you need to configure each service accordingly.

```yaml
telegram:
  chatID: %YOUR_TELEGRAM_CHAT_ID%
  botToken: %YOUR_TELEGRAM_BOT_TOKEN%
  webhookURL: https://%YOUR_OWN_TELEGRAM_BOT_DOMAIN%"

webhooks:
  - name: ko-fi.com
    pattern: ko-fi # Will be added to your domain as /webhooks/ko-fi as the WebhookURL -> https://%YOUR_OWN_TELEGRAM_BOT_DOMAIN%/webhooks/ko-fi 
    contentType: application/x-www-form-urlencoded # Can be either "application/x-www-form-urlencoded" or "application/json"
    formKey: data # Name of the form field which contains the json data -> Required only for contentType: application/x-www-form-urlencoded
    verification: # Here we validate the message and compare the key "verification_token" against the value we defined at ko-fi.com
      type: message
      key: verification_token
      value: "abcd-11111-2222-3333-4444-5555"
    templates:
      - template: "Kofi-%s:\n\n%s (%s) donates %s %s to you!\n\nTimestamp: %s\nURL:%s\nMessage:\n%s"
        keys:
          - type
          - from_name
          - email
          - amount
          - currency
          - timestamp
          - url
          - message

  - name: github.com (Repository Stars)
    pattern: github/stars # Will be added to your domain as /webhooks/github/stars as the WebhookURL -> https://%YOUR_OWN_TELEGRAM_BOT_DOMAIN%/webhooks/github/stars 
    contentType: application/json # Can be either "application/x-www-form-urlencoded" or "application/json"
    #telegramChatID: %YOUR_TELEGRAM_CHAT_ID% # You can overwrite the chatID if necessary.
    verification: # Here we validate the http "X-GitHub-Hook-ID" header and compare against the id of our GitHub hook.
      type: header
      key: X-GitHub-Hook-ID
      value: %THE_ID_OF_YOUR_GITHUB_HOOK%
    templates:
      - template: "Github-Event: %s | Action: %s\n\nYour repository %q got a new star!\nIt has now %.f stars."
        keys:
          - header:X-GitHub-Event # Use "header:HTTP_HEADER_NAME" if you want to access the values from http header instead of the values from the message itself. 
          - action
          - repository.name
          - repository.stargazers_count
        trigger: # Optional: A key value pair which triggers this template (only messages with action=created trigger this template)
          type: message # Take the value either from the message or from the header for comparing with value
          key: action
          value: created

      - template: "Github-Event: %s | Action: %s\n\nYour repository %q lost a star😢\nIt has now %.f stars."
        keys:
          - header:X-GitHub-Event
          - action
          - repository.name
          - repository.stargazers_count
        trigger: # Optional: A key value pair which triggers this template (only messages with action=deleted trigger this template)
          type: message # Take the value either from the message or from the header for comparing with value
          key: action
          value: deleted
```

## General considerations
Since there exists no "Webhook" standard yet and because I build this little project for myself and the only webhooks I currently use are the one from ko-fi.com and github.com, 
probably not all available use cases for webhook messages will be supported "out-of-the-box". If you have a special case or need any help, create an issue and I try to help. 
If you would like to contribute, I'll be happy to review your PR.

## Support
If you like the project and find it useful, I'd be grateful if you [buy me a ☕](https://ko-fi.com/callmemisterk).