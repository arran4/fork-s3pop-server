# Receiving email with S3 or Cloudflare R2

S3 POP3 Server is a read-only object-storage consumer, with a stateful local POP3 spool. It is not an SMTP receiver, sender, or mail-routing service. The upstream delivery service must write one complete raw email per object. Keep the bucket private.

> **Security:** The current POP3 implementation listens on all interfaces, does not implement TLS, accepts PASS without checking the password, and does not authorize usernames. It also logs command arguments. Use only on a trusted machine/network, restrict access with a firewall or loopback-only container port publishing, and do not supply a real password. Do not expose this service to untrusted clients or use it to isolate mutually untrusted users. Multiple mailbox prefixes are a convenience, not a security boundary.

## Mailbox storage contract

For a bucket named `my-email-bucket`, an example layout is:

```text
my-email-bucket/
  alice@example.com/
    7615d78d-029b-44c3-97de-77ae8e0061e3.eml
    3008a6a2-b3b6-4a73-9a85-a0b2f7f44d20.eml
  bob@example.com/
    96994350-039e-4ec8-bf19-2b4f5db4bade.eml
```

- Set the POP3 username to the mailbox prefix without the trailing slash (e.g. `alice@example.com`). Use a simple safe single directory name, not a path; avoid spaces, control characters, slashes, backslashes, `.` and `..`.
- Each object must contain the original RFC 5322 message, including all headers, a blank line, and the MIME body with attachments. No JSON envelope, base64 wrapping of the entire message, compression, or separate attachment objects. `.eml` is optional; SES-generated identifiers without extensions work. Content-Type `message/rfc822` is helpful but not required.
- Use immutable, unique, filesystem-safe basenames (e.g. UUIDs or SES-generated IDs). Do not use a user-supplied Message-ID or Subject as a filename. Avoid names ending in `.json`, the reserved local name `_email_index.txt`, directory marker objects and any unrelated files.
- The backend passes the username as the exact **string prefix** to ListObjectsV2, with no added slash or delimiter. Thus `alice@example.com` also matches `alice@example.com.other/...`. Choose non-overlapping mailbox prefixes and do not store other objects under matching prefixes. Use the slash-separated layout for writers, but note that it does not enforce isolation in the reader.
- Subdirectories are not represented in POP3. The backend flattens keys using their basename; equal basenames in different subdirectories collide. Prefer a single level of objects beneath each mailbox.
- The current implementation reads only one ListObjectsV2 page (normally at most 1,000 keys). Even previously downloaded objects count towards that page. Large retained mailboxes may therefore prevent new messages being discovered. Do not treat this as a complete archive scanner; pagination requires a code fix.
- The object body is processed using Go's line scanner; unusually long individual lines can fail. Standard MIME transfer encodings with normal line wrapping are recommended.

A minimal valid test email (`test.eml`):

```eml
From: sender@example.net
To: alice@example.com
Subject: S3 POP3 test
Date: Thu, 26 Nov 2026 12:00:00 +1100
Message-ID: <test-1@example.net>
MIME-Version: 1.0
Content-Type: text/plain; charset=UTF-8

Test message.
```

Upload it as `alice@example.com/test-1.eml` using the provider console or CLI with a separate writer credential. Do not upload a folder-marker object. Test using a fresh mailbox/prefix, then connect a POP3 client.

## Local state and migration

The server downloads matching objects at the POP3 USER command, before PASS. It maintains a persistent local directory under the operating-system account running the process (from `os/user.Current()`, not a configurable cache path):

```text
~/.email/
  alice@example.com/
    _email_index.txt
    test-1.eml
    test-1.eml.json
```

The index is an append-only newline-delimited list of object basenames previously downloaded. The JSON sidecar records `id`, `name`, `headerSize`, `messageSize`, `totalSize` and `read`. The raw local file retains the original downloaded bytes; the POP3 UIDL returned to clients is the basename, not the numeric ID or RFC Message-ID.

POP3 DELE followed by QUIT removes the local message and its JSON sidecar but retains the index entry and remote object. A subsequent connection does not normally re-download that basename. Changing a remote object in place is not detected. Removing the remote object does not remove an already downloaded local message. Removing local state can cause retained messages to be imported again; keeping the index without the remaining spool can suppress undelivered messages.

Back up and migrate the entire mailbox directory, including the index, with the server stopped. Preserve filenames, case, ownership and permissions. When changing provider, retain the same mailbox prefix and basenames if continuing with the same state. Do not share one state directory between independent server instances or simultaneously access one mailbox from multiple clients; there is no mailbox locking. Do not switch buckets while reusing unrelated local state for the same username. Keep independent backups and configure provider retention deliberately; POP3 deletion will not reclaim remote storage.

## AWS S3 with SES email receiving

1. Select an [SES email receiving region](https://docs.aws.amazon.com/general/latest/gr/ses.html), verify your domain in SES, and publish the required inbound MX record (for that region) and domain verification DNS records. Your DNS can be hosted by any provider. For this example use `us-east-1`.
2. Create a private general-purpose S3 bucket in the same region, with Block Public Access enabled and ordinary S3-managed encryption (SSE-S3). Do not enable SES receipt-action client-side KMS encryption: the POP3 backend expects raw MIME and does not implement that decryption.
3. Grant the SES service permission to deposit messages as described below. Create and activate a receipt rule set. For each mailbox, configure a recipient condition matching that envelope address and a Deliver to S3 action with bucket `my-email-bucket` and object key prefix `alice@example.com/` (include the slash). SES appends its own message identifier. Repeat for other recipients with distinct prefixes; a domain-wide rule with a single fixed prefix creates a shared inbox, not separate mailboxes. Review rule ordering and behavior for messages with multiple recipients.
4. Create a separate runtime IAM principal with only the read permissions below. Prefer an execution role with short-lived credentials when supported by your environment, otherwise use a dedicated IAM user's access keys, not root or administrator credentials.
5. Configure and start the POP3 server as described below.

### SES delivery permission (S3 bucket policy)

Example for classic SES receipt rules in the same account and region. Replace the bucket, region, account ID, rule set, and receipt rule name. Merge the statement into an existing bucket policy rather than replacing unrelated statements. Repeat with the correct resource and source ARN for additional rules.

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Sid": "AllowSesAliceDelivery",
      "Effect": "Allow",
      "Principal": {"Service": "ses.amazonaws.com"},
      "Action": "s3:PutObject",
      "Resource": "arn:aws:s3:::my-email-bucket/alice@example.com/*",
      "Condition": {
        "StringEquals": {
          "AWS:SourceAccount": "123456789012",
          "AWS:SourceArn": "arn:aws:ses:us-east-1:123456789012:receipt-rule-set/mail-rules:receipt-rule/alice"
        }
      }
    }
  ]
}
```

If SES validates the S3 action with a test object or requires permissions outside the chosen prefix, check the actual object key/CloudTrail and adjust the policy narrowly as necessary; do not broaden the reader policy. Other arrangements (cross-account buckets, different regions, IAM role-based delivery or KMS) may need additional permissions. See [SES receiving permissions](https://docs.aws.amazon.com/ses/latest/dg/receiving-email-permissions.html) and [Deliver to S3 bucket](https://docs.aws.amazon.com/ses/latest/dg/receiving-email-action-s3.html).

### POP3 reader IAM policy

Attach this identity policy to the dedicated reader role or user. This example allows listing exactly the prefixes actually passed by this implementation and reading only message objects beneath the intended mailbox directories. A non-overlapping naming convention is still necessary; a prefix-only list condition does not filter individual matching keys.

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Sid": "ListMailboxes",
      "Effect": "Allow",
      "Action": "s3:ListBucket",
      "Resource": "arn:aws:s3:::my-email-bucket",
      "Condition": {
        "StringEquals": {
          "s3:prefix": ["alice@example.com", "bob@example.com"]
        }
      }
    },
    {
      "Sid": "ReadMessages",
      "Effect": "Allow",
      "Action": "s3:GetObject",
      "Resource": [
        "arn:aws:s3:::my-email-bucket/alice@example.com/*",
        "arn:aws:s3:::my-email-bucket/bob@example.com/*"
      ]
    }
  ]
}
```

For one server per bucket, a simpler bucket-scoped policy can omit the ListBucket condition and use `arn:aws:s3:::my-email-bucket/*` for GetObject. Neither PutObject nor DeleteObject is needed by the reader. HeadObject, if used by the SDK, is covered by GetObject. SSE-KMS storage may require additional KMS permissions; use SSE-S3 for this example. See [S3 IAM examples](https://docs.aws.amazon.com/AmazonS3/latest/userguide/example-policies-s3.html).

## Cloudflare Email Routing with R2

Cloudflare Email Routing can hand an incoming message to an Email Worker. R2 alone does not accept incoming SMTP and routing does not automatically deposit raw emails in R2. The following is a minimal single-domain example; no outbound Email Sending subscription is required for this routing design. R2 and Workers usage limits and billing still apply.

1. Add your domain to Cloudflare and enable Email Routing, following its DNS instructions. Review existing MX records before changing a domain's delivery provider.
2. Create a private R2 bucket `my-email-bucket`. Leave public access disabled.
3. Create a Worker with an email handler, and add an R2 bucket binding named `MAIL_BUCKET` pointing to that bucket (Dashboard: Worker settings/bindings, or Wrangler configuration below).
4. Deploy the Worker. In Email Routing, configure explicit custom address rules for `alice@example.com` and `bob@example.com` to send to this Worker. Do not enable catch-all unless you intentionally handle it.
5. In the R2 API token management page, create a token with **Object Read only**, scoped to this bucket. Copy the generated S3 Access Key ID, Secret Access Key and account endpoint. These are not a general Cloudflare API bearer token. The Worker writes through its R2 binding and does not need those credentials. For untrusted mailbox isolation, use separate buckets and separate deployments; the reader credential is bucket-scoped.
6. Configure the POP3 server with the R2 endpoint, path style and region `auto` as described below.

### Example Worker (index.js)

Explicitly map accepted envelope recipients to safe fixed prefixes. Do not use the MIME To header, since it can differ from the delivered recipient (e.g. Bcc). Add aliases to the map deliberately if they should feed the same mailbox.

```javascript
const mailboxes = new Map([
  ["alice@example.com", "alice@example.com"],
  ["bob@example.com", "bob@example.com"],
]);

export default {
  async email(message, env) {
    const mailbox = mailboxes.get(message.to);
    if (!mailbox) {
      message.setReject("Unknown mailbox");
      return;
    }

    const key = mailbox + "/" + crypto.randomUUID() + ".eml";
    // Preserve the MIME bytes. Buffering also gives R2 a known body length.
    const raw = await new Response(message.raw).arrayBuffer();
    await env.MAIL_BUCKET.put(key, raw, {
      httpMetadata: { contentType: "message/rfc822" },
    });
  },
};
```

Example `wrangler.toml` (or configure the equivalent binding in the dashboard):

```toml
name = "mail-to-r2"
main = "index.js"
compatibility_date = "2026-11-26"

[[r2_buckets]]
binding = "MAIL_BUCKET"
bucket_name = "my-email-bucket"
```

For Wrangler deployment, authenticate and run `npx wrangler deploy` from the directory containing these two files, then configure the email routes. This is a minimal example, not a production queue: it buffers the whole message in memory, uses one UUID per delivery invocation (retries may create duplicate objects), and awaits the write without swallowing errors. Test message sizes within Worker limits and confirm delivery failure behavior for your deployment before relying on it. For large messages, consider known-length streaming or a durable ingestion design. The example does not forward mail; use a separate test address before switching existing routes.

See [Email Routing setup](https://developers.cloudflare.com/email-service/get-started/route-emails/), [email handler API](https://developers.cloudflare.com/email-service/api/route-emails/email-handler/), [R2 binding API](https://developers.cloudflare.com/r2/api/workers/workers-api-reference/) and [R2 token setup](https://developers.cloudflare.com/r2/api/tokens/).

## POP3 server configuration

On the machine running the server, create an AWS SDK config file at the OS account's home directory `~/.aws/config` (the current backend explicitly checks this file exists, even if SDK credentials and region are provided through environment variables). For AWS use:

```ini
[default]
region = us-east-1
```

For R2 use:

```ini
[default]
region = auto
```

If using static credentials, put them in `~/.aws/credentials` or a file selected by `AWS_SHARED_CREDENTIALS_FILE`. The file format is identical for AWS and R2:

```ini
[default]
aws_access_key_id = REPLACE_WITH_ACCESS_KEY_ID
aws_secret_access_key = REPLACE_WITH_SECRET_ACCESS_KEY
```

Restrict access to both files; never commit real credentials. For temporary AWS credentials include `aws_session_token` when required, or use the SDK's normal role credential provider.

AWS `server-config.json`:

```json
{
  "port": 5110,
  "s3Bucket": "my-email-bucket"
}
```

Cloudflare R2 `server-config.json`:

```json
{
  "port": 5110,
  "s3Bucket": "my-email-bucket",
  "s3Endpoint": "https://ACCOUNT_ID.r2.cloudflarestorage.com",
  "s3ForcePathStyle": true
}
```

Use the actual S3 endpoint supplied by R2, including any jurisdiction-specific endpoint where applicable. Do not append a bucket name or use an R2 public URL. Ensure no stale AWS endpoint/credential environment variables override the desired configuration.

Start `./s3pop-server -config server-config.json`. Restrict the listening port before starting a native deployment. For containers see the [README](../README.md#docker) and persist the state directory.

## Verify delivery and retrieval

1. Send a test message to the configured recipient (preferably including an attachment). Confirm a raw object exists under the exact mailbox prefix.
2. With the reader credentials and correct region, check listing and retrieval. For AWS, omit the endpoint option; for R2 set it to the account endpoint:

   ```sh
   aws --endpoint-url https://ACCOUNT_ID.r2.cloudflarestorage.com s3api list-objects-v2 \
     --bucket my-email-bucket --prefix alice@example.com --region auto
   ```

   Download a listed object with `aws s3api get-object --bucket my-email-bucket --key 'alice@example.com/ACTUAL_OBJECT_KEY' downloaded.eml` plus the same region and, for R2, endpoint option. Keep any downloaded mail private.
3. Configure a POP3 client for host `127.0.0.1`, port 5110, username `alice@example.com`, no TLS on the local trusted connection and a disposable dummy password if required.
4. Check that headers, body and attachment arrive. Reconnect, confirm the UIDL remains stable, and test local deletion with a disposable message. Verify the remote object remains intact. Restart the server with the same persistent local state and verify a deleted message does not reappear.
5. If no messages arrive, check the prefix, region, credentials, mandatory AWS config file, state index, listing page limit and server logs. Avoid logging/sharing sensitive email contents.

Sending mail is a separate concern; configure an SMTP provider independently.
