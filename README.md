# whtt — Webhook to Telegram

[![Go Report Card](https://goreportcard.com/badge/github.com/sknr/webhook-to-telegram)](https://goreportcard.com/report/github.com/sknr/webhook-to-telegram)
![GitHub go.mod Go version](https://img.shields.io/github/go-mod/go-version/sknr/webhook-to-telegram?style=flat)
![GitHub License](https://img.shields.io/github/license/sknr/webhook-to-telegram)
[![Sponsor me on GitHub](https://img.shields.io/badge/Sponsor%20me%20on%20GitHub-sknr-ea4aaa?style=flat&logo=github-sponsors)](https://github.com/sponsors/sknr)

**whtt** is a lightweight, high-performance, and secure Telegram bot server that forwards and formats webhook updates from services like GitHub, Ko-fi, PayPal, Stripe, and custom backends straight to your Telegram chats. 

**Zero coding required.** Simply write a YAML configuration, launch the binary, and start receiving beautiful, formatted alerts immediately.

---

## Why Choose whtt?

*   🚀 **No Coding Needed**: Configure routes, verify headers, and parse multi-nested JSON payloads into human-readable Telegram alerts using only a simple `config.yml` file.
*   🔒 **IETF Secure Webhook Token (SWT) Support**: Future-proof security built-in. Native support for the upcoming [IETF Secure Webhook Token draft standard](https://github.com/SecureWebhookToken/swt) (`draft-knauer-secure-webhook-token-02`) to cryptographically verify webhook signatures and guarantee request body integrity with SHA-256 or SHA-512 hashes.
*   📝 **Rich Message Templates**: Support for `HTML`, `Markdown`, and `MarkdownV2` styling with automatic character escaping so your messages never break.
*   ⚡ **Smart Trigger Filtering**: Route only the events you care about. Define custom triggers to execute specific templates when fields in the webhook JSON payload match your criteria (e.g. only notify on `"action": "created"`).
*   📦 **Zero External Dependencies**: Compiled into a single, Go-native static binary with zero external runtime dependencies. Minimal memory footprint and instant startup.

---

## Quick Start in 3 Steps

1.  **Create your Telegram Bot**: Message [@BotFather](https://t.me/botfather) on Telegram, create a new bot, and get your bot token.
2.  **Configure**: Copy `config_example.yml` to `config.yml` and add your Telegram bot token, chat ID, and desired webhook rules.
3.  **Run**:
    *   **Option A: Run locally**
        ```bash
        go build cmd/main.go -o whtt
        ./whtt
        ```
    *   **Option B: Run with Docker Compose**
        ```bash
        docker compose up -d --build
        ```

---

## Example `config.yml`

Configure multiple incoming webhooks with custom verification methods (No validation, Header check, Message payload check, or Secure Webhook Tokens):

```yaml
telegram:
  chatID: 987654321                         # Default Chat ID to receive messages
  botToken: "123456789:ABCdefGhIJKlmNoPQ"   # Your Telegram Bot Token
  webhookURL: "https://yourdomain.com/telegram-webhook"

webhooks:
  # 1. Ko-fi Webhook (x-www-form-urlencoded payload & simple verification token)
  - name: ko-fi.com
    pattern: ko-fi                          # Handled at: https://yourdomain.com/webhooks/ko-fi
    contentType: application/x-www-form-urlencoded
    formKey: data                           # Key containing the JSON body
    verification:
      type: message
      key: verification_token
      value: "your-kofi-verification-token"
    templates:
      - template: "☕ <b>Ko-fi Donation!</b>\n\n%s (%s) donated %s %s!\n\nMessage: <i>%s</i>"
        keys:
          - from_name
          - email
          - amount
          - currency
          - message

  # 2. GitHub Stars Webhook (JSON payload, HMAC-SHA256 verification & triggers)
  - name: github.com (Repository Stars)
    pattern: github/stars                   # Handled at: https://yourdomain.com/webhooks/github/stars
    contentType: application/json
    verification:
      type: hmac-sha256
      key: X-Hub-Signature-256
      value: "your-github-webhook-secret-token"
    templates:
      - template: "⭐ <b>GitHub Star Added!</b>\n\nRepository: <code>%s</code>\nTotal Stars: %.f"
        keys:
          - repository.name
          - repository.stargazers_count
        trigger:
          type: message
          key: action
          value: created
      - template: "💔 <b>GitHub Star Removed</b>\n\nRepository: <code>%s</code>\nTotal Stars: %.f"
        keys:
          - repository.name
          - repository.stargazers_count
        trigger:
          type: message
          key: action
          value: deleted

  # 3. Secure Webhook Token (IETF SWT Draft standard)
  - name: secure-service.com (SWT Verification)
    pattern: secure-service                 # Handled at: https://yourdomain.com/webhooks/secure-service
    contentType: application/json
    verification:
      type: swt
      value: "my-shared-secret-key-used-for-signing-swt-token"
    templates:
      - template: "🔒 <b>Secure Event [%s]</b>\n\nPayment from %q was successful.\nRetry count: %v"
        keys:
          - swt:event                       # Access the verified SWT event claim
          - customer.name
          - swt:retry_count                 # Access the verified SWT retry count claim
        trigger:
          type: message
          key: swt:event
          value: payment.completed
```

---

## Configuration Reference

The `config.yml` file is structured into two main components: `telegram` (global settings) and `webhooks` (routing, security, and template formatting rules).

### 1. Global Settings (`telegram`)

| Option | Type | Required | Description |
| :--- | :--- | :--- | :--- |
| `botToken` | String | Yes | Your Telegram Bot token obtained from [@BotFather](https://t.me/botfather). |
| `chatID` | Integer | Yes | Default Telegram Chat or Group ID to send messages to. |
| `webhookURL` | String | Yes | Your server's public Telegram webhook endpoint URL (e.g. `https://yourdomain.com/telegram-webhook`). |

---

### 2. Webhook Rules (`webhooks`)

Define a list of incoming endpoints. Each entry supports the following fields:

| Option | Type | Default | Description |
| :--- | :--- | :--- | :--- |
| `name` | String | | A descriptive label for this webhook (e.g., `GitHub Stars`). |
| `pattern` | String | | The URL route path suffix. Handled under `/webhooks/<pattern>`. |
| `contentType` | String | `application/json` | Expected HTTP content type: `application/json` or `application/x-www-form-urlencoded`. |
| `formKey` | String | `payload` | *(For URL-encoded requests)* The form parameter key containing the JSON string. |
| `parseMode` | String | `none` | Markdown/HTML formatting mode: `html`, `markdown`, or `markdownv2` (case-insensitive). |
| `telegramChatID`| Integer | *(global chatID)* | Override the global chat ID to send these specific events to a different chat/group. |
| `verification` | Object | `none` | Security authentication parameters (see below). |
| `templates` | Array | | Formatting and routing rules for generating the Telegram alerts. |

#### Webhook Authentication (`verification`)
Prevent unauthorized request forwarding using one of the following authentication methods configured via the `verification` object:

| `type` | Description |
| :--- | :--- |
| `none` | No signature verification or validation is performed. |
| `header` | Compares the value of the HTTP request header specified in `key` against the configured `value` (e.g. `key: X-GitHub-Hook-ID`). |
| `message` | Compares the value of a JSON key/field in the payload (specified in `key`) against the configured `value`. |
| `hmac-sha256` | HMAC-SHA256 signature verification. Automatically trims the `sha256=` prefix (useful for GitHub/Gitea). The `key` is the header name, and the `value` is the shared secret. |
| `hmac-sha1` | HMAC-SHA1 signature verification. Automatically trims the `sha1=` prefix. The `key` is the header name, and the `value` is the shared secret. |
| `hmac-sha256-base64` | HMAC-SHA256 signature verification with base64-encoded signature (useful for Shopify). The `key` is the header name, and the `value` is the shared secret. |
| `hmac-sha1-base64` | HMAC-SHA1 signature verification with base64-encoded signature. The `key` is the header name, and the `value` is the shared secret. |
| `swt` | Future-proof IETF Secure Webhook Token signature verification using HMAC-SHA256 and body integrity hashing. The `value` is the shared secret. |

---

### 3. Message Templates (`templates`)

Templates define what text is sent to Telegram, what parameters are extracted, and when the message is triggered.

| Field | Type | Description |
| :--- | :--- | :--- |
| `template` | String | A Go-style format string using placeholders (e.g., `%s` for strings, `%d` for integers, `%.f` for floats, `%v` for generic/any value). |
| `keys` | Array of Strings | Ordered list of keys to fetch values for replacing the placeholders in the template. (See Key Selector Syntax below). |
| `trigger` | Object | *(Optional)* Filters when this template is used. Executes only if the trigger key value matches the target value. |

#### Key Selector Syntax (`keys` & `trigger.key`)
To extract values dynamically, use these selector types in your `keys` list or triggers:
1.  **Plain/Nested Keys**: Access JSON properties. Use dot-notation for nested structures.
    *   *Example:* `action` (resolves to top-level `"action"` field)
    *   *Example:* `repository.name` (resolves to `repository: { name: "..." }`)
2.  **HTTP Headers**: Read the request headers by prefixing with `header:`.
    *   *Example:* `header:X-GitHub-Event`
3.  **SWT Claims**: Read verified values from the Secure Webhook Token context by prefixing with `swt:`.
    *   *Example:* `swt:event` (the verified webhook event type)
    *   *Example:* `swt:retry_count` (the verified webhook retry count)

---

## General Considerations

Since webhooks vary between services, we built a highly flexible parser. If your webhook service requires a special formatting scheme or verification type that is not yet supported, please feel free to open an issue or submit a pull request!

---

## Support

If you like the project and find it useful, please consider sponsoring me on GitHub! Your support helps keep this tool maintained and secure.

[![Sponsor me on GitHub](https://img.shields.io/badge/Sponsor%20me%20on%20GitHub-sknr-ea4aaa?style=for-the-badge&logo=github-sponsors)](https://github.com/sponsors/sknr)