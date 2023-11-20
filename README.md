# Webhook to Telegram message

![Go Report Card](https://goreportcard.com/report/github.com/sknr/webhook-to-telegram)
![GitHub go.mod Go version](https://img.shields.io/github/go-mod/go-version/sknr/webhook-to-telegram?style=flat)
![GitHub Licence](https://img.shields.io/github/license/sknr/webhook-to-telegram)

A Telegram bot which may handle webhook updates from several 
services like GitHub, Kofi, PayPal, etc.

## Required steps

1. Create a Telegram bot via [BotFather](https://t.me/botfather) and obtain a bot token.
2. Edit your `.env` file or your env vars directly and set both `BOT_TOKEN` and `TELEGRAM_WEBHOOK_URL` accordingly.
3. Copy or rename `webhooks_example.yml` to `webhooks.yml` and configure to your desired webhook service needs. (See ko-fi.com example)
4. Build cmd/main.go via `go build main.go -o wh2t` and run the Telegram bot server.

## Example: webhooks.yml

In order to turn json data send via webhook to your Telegram bot into a message send to your Telegram account, you need to configure each service accordingly.

```yaml
- name: ko-fi.com
  pattern: ko-fi # The pattern under which the webhook will be accessible -> (https://YOURDOMAIN.COM/webhooks/ko-fi)
  contentType: application/x-www-form-urlencoded
  formKey: data # Name of the form field which contains the json data -> Required only for contentType: application/x-www-form-urlencoded
  tokenMessageKey: verification_token
  token: %YOUR_KOFI_VERIFICATION_TOKEN%
  telegramChatID: %YOUR_TELEGRAM_CHAT_ID%
  messageTemplate: "Kofi-%s:\n%s (%s) donates %s %s to you!\n\n\nType: %s\nTimestamp: %s\nURL:%s\nMessage:\n%s"
  messageKeys: # The ordering of the messageKeys is important and needs to be in order of the usage within the MessageTemplate
    - type
    - from_name
    - email
    - amount
    - currency
    - timestamp
    - url
    - message
```

### Config keys 

#### name
Is just a naming of this webhook configuration

---

#### pattern
The pattern key describes the last part under which your webhook will be accessible. 

For example:
> Your `TELEGRAM_WEBHOOK_URL=https://telegram.example.com/webhooks` and the pattern is `ko-fi` as stated in the example above,
> then the resulting webhook URL you would enter in your Kofi-profile would be `https://telegram.example.com/webhooks/ko-fi`.

---

#### contentType
Can either be `application/x-www-form-urlencoded` or `application/json` depending on the service you use.

---

#### formKey
Is required only for `contentType=application/x-www-form-urlencoded` in order to specify the form key for retrieving the json data.

---

#### tokenMessageKey
The `tokenMessageKey` defines the name of the message key which contains the token for validating that the source of the call is what you expect it to be.

---

#### token
Defines the actual token provided by the webhook service in use. If it does not match the value retrieved via the `tokenMessageKey`, the webhook message will be rejected.

---

#### telegramChatID
Your telegram userID or groupID (also referred to as chatID) you want your Telegram bot interact with.
> If you don't know your telegram chatID yet, you can simply start your Telegram bot server via `./wh2t` and send `/id` as a telegram message to your bot.
> The telegram bot will respond with your chatID.

---

#### messageTemplate
The `messageTemplate` key is used to define the message format in the [*go fmt*](https://pkg.go.dev/fmt) syntax, which will be sent as a Telegram message to you.
The placeholders you specify in the template will be replaced with the specified `messageKeys`

---

#### messageKeys
Define all message keys for which you want their values to replace the placeholders of your specified `.messageTemplate`.
Multiple usage of the same key (if required) is possible.
> The order of the keys is important and should match the order of the placeholder in your `messageTemplate`.

## General considerations
Since there exists no "Webhook" standard yet and because I build this little project for myself and the only webhook I currently use is the one from ko-fi.com, 
probably not all available use cases for webhook messages will be supported "out-of-the-box". If you have a special case or need any help, create an issue and I try to help. 
If you would like to contribute, I'll be happy to review your PR.

## Support
If you like the project and find it useful, I'd be grateful if you [buy me a ☕](https://ko-fi.com/callmemisterk).