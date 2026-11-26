# S3 POP3 Server

This program is part of a solution to allow you to use AWS S3 or S3-compatible services (like Cloudflare R2) to provide yourself with a cheap and functional custom email address. It enables you to read email delivered to an S3 bucket by a routing service like Amazon Web Services (AWS) Simple Email Service (SES) or Cloudflare Email Routing in a standard mail reader such as Thunderbird or Windows Mail.
The program works by running a local POP3 server which downloads emails stored in raw MIME format in an S3 Bucket and presents them to your mail client as if it were a standard POP3 email server.

Installation instructions are below. To use the application once installed you simply need to make sure it is running when you check your email with your mail client. 

Setting up your email in this way could work for you if some or all of the below are true:
- You already use (or want to use) AWS S3 or an S3-compatible service (like Cloudflare R2) to host your website or store your data.
- Your budget is limited or you want to save money.
- You mainly want to access your email from one computer (not your phone.) Currently if you are using this program  marking your emails read on one computer would not mark them as read on another. There is currently no mobile version of this app available though if you would like to help write one pull requests are welcome. 
- You want to separate your email receiving and storing infrastructure from your sending provider. Note that this server only handles receiving email (POP3); you must handle email sending (SMTP) through your chosen provider.
- You either have some technical nouse yourself or know someone who does and who is willing to help you set this up.

You can contact me by email  on fractal dot mango at gmail dot com if you have questions.

If you want someone to set this up for you I am happy to do so for a small fee.

## Setup

Choose a receiving provider and follow the [complete receiving and storage guide](docs/receiving-email.md), which includes:

- AWS SES to S3 delivery, DNS and receipt rules, with separate least-privilege IAM examples for the SES writer and POP3 reader.
- Cloudflare Email Routing to an Email Worker to R2, including an example Worker, R2 binding and read-only API token setup.
- The mailbox prefix and raw MIME object layout, stable filenames, deduplication, local state and migration requirements.
- Native AWS and R2 configuration, limitations and a test procedure.

**Security:** This server listens on all interfaces, does not validate POP3 passwords or implement TLS, and does not authorize mailbox usernames. It logs POP3 command arguments. Restrict native deployments with a firewall and publish container ports only on loopback. Do not use real passwords, expose this service to untrusted clients or treat mailbox prefixes as an authorization boundary.

Download a release, copy `server-config.json.example` to `server-config.json`, and configure it for your chosen provider using the guide (remove the R2 endpoint and path-style settings for standard AWS S3). Start the server:

```bash
./s3pop-server -config server-config.json -port 5110
```

Use `./s3pop-server --help` for the available command-line arguments. The configuration path and port can be overridden from the command line; environment variables override the applicable JSON fields. The backend also requires the OS account's `~/.aws/config` file to exist, even with environment credentials. Preserve `~/.email` between runs.

### Docker
Alternatively, you can use the Docker image. You can pull the latest image with:
```bash
docker pull ghcr.io/arran4/s3pop-server:latest
```

Before using any of the following Docker or Compose examples, create a file named `aws-config` beside your command/Compose file containing `[default]` and `region = us-east-1` (AWS; substitute your region) or `region = auto` (R2). The image runs as the non-root `s3pop` user with home and working directory `/home/s3pop`. The config file must be mounted at `/home/s3pop/.aws/config`, even when credentials and region are provided via environment variables.

Create a persistent directory named `s3pop-state` for the local spool and index. Ensure it is writable by the container user, and that configuration/credential files are readable by that user but not exposed to other users. Use `docker run --rm --entrypoint id ghcr.io/arran4/s3pop-server:latest` to identify its UID/GID (account for user namespace mappings if applicable). Each independent deployment needs its own state directory. Back it up; removing it can cause previously downloaded remote messages to reappear.

All examples below publish only on loopback because the service lacks authentication and TLS. Prefer the credentials-file examples over placing credentials in environment variables.

Configuration can be passed via environment variables instead of (or overriding) the JSON configuration file:

| Environment Variable | Description | Example |
|---|---|---|
| `S3POP_PORT` | The port the POP3 server will listen on (default 5110). | `110` |
| `S3POP_S3_BUCKET` | The name of the S3 bucket to read emails from. | `my-email-bucket` |
| `S3POP_S3_ENDPOINT` | Custom S3 endpoint URL (e.g. for Cloudflare R2). | `https://account-id.r2.cloudflarestorage.com` |
| `S3POP_S3_FORCE_PATH_STYLE` | Force path style URLs for S3 operations (`true`/`false`). | `true` |
| `S3POP_CONFIG` | Path to a custom JSON configuration file (defaults to `./server-config.json` in the working directory (`/home/s3pop` in the image)). | `/etc/s3pop/server-config.json` |

#### Docker Run Examples

**Standard AWS S3**
```bash
docker run -d --rm --name s3pop-server -p 127.0.0.1:5110:5110 \
  -v "$PWD/aws-config:/home/s3pop/.aws/config:ro" \
  -v "$PWD/s3pop-state:/home/s3pop/.email" \
  -e S3POP_S3_BUCKET="my-email-bucket" \
  -e AWS_ACCESS_KEY_ID="your_access_key" \
  -e AWS_SECRET_ACCESS_KEY="your_secret_key" \
  -e AWS_REGION="us-east-1" \
  ghcr.io/arran4/s3pop-server:latest
```

**Cloudflare R2**
```bash
docker run -d --rm --name s3pop-server -p 127.0.0.1:5110:5110 \
  -v "$PWD/aws-config:/home/s3pop/.aws/config:ro" \
  -v "$PWD/s3pop-state:/home/s3pop/.email" \
  -e S3POP_S3_BUCKET="my-email-bucket" \
  -e S3POP_S3_ENDPOINT="https://<ACCOUNT_ID>.r2.cloudflarestorage.com" \
  -e S3POP_S3_FORCE_PATH_STYLE="true" \
  -e AWS_ACCESS_KEY_ID="your_r2_access_key" \
  -e AWS_SECRET_ACCESS_KEY="your_r2_secret_key" \
  -e AWS_REGION="auto" \
  ghcr.io/arran4/s3pop-server:latest
```



#### Docker Run Examples (with Docker Secrets / Mounts)

**Standard AWS S3**
```bash
docker run -d --rm --name s3pop-server -p 127.0.0.1:5110:5110 \
  -v "$PWD/aws-config:/home/s3pop/.aws/config:ro" \
  -v "$PWD/s3pop-state:/home/s3pop/.email" \
  -e S3POP_S3_BUCKET="my-email-bucket" \
  -e AWS_REGION="us-east-1" \
  -e AWS_SHARED_CREDENTIALS_FILE="/run/secrets/aws_credentials" \
  -v ./my_aws_credentials:/run/secrets/aws_credentials:ro \
  ghcr.io/arran4/s3pop-server:latest
```

**Cloudflare R2**
```bash
docker run -d --rm --name s3pop-server -p 127.0.0.1:5110:5110 \
  -v "$PWD/aws-config:/home/s3pop/.aws/config:ro" \
  -v "$PWD/s3pop-state:/home/s3pop/.email" \
  -e S3POP_S3_BUCKET="my-email-bucket" \
  -e S3POP_S3_ENDPOINT="https://<ACCOUNT_ID>.r2.cloudflarestorage.com" \
  -e S3POP_S3_FORCE_PATH_STYLE="true" \
  -e AWS_REGION="auto" \
  -e AWS_SHARED_CREDENTIALS_FILE="/run/secrets/aws_credentials" \
  -v ./my_r2_credentials:/run/secrets/aws_credentials:ro \
  ghcr.io/arran4/s3pop-server:latest
```

#### Docker Compose Examples (with Credentials File)

The AWS SDK for Go v2 supports the standard `AWS_SHARED_CREDENTIALS_FILE` environment variable ([see AWS SDK Docs](https://aws.github.io/aws-sdk-go-v2/docs/configuring-sdk/#environment-variables)). You can use this to securely mount and pass your AWS credentials without exposing them directly in your compose file.

**Standard AWS S3**
```yaml
version: '3.8'

services:
  s3pop-server:
    image: ghcr.io/arran4/s3pop-server:latest
    ports:
      - "127.0.0.1:5110:5110"
    environment:
      - S3POP_S3_BUCKET=my-email-bucket
      - AWS_REGION=us-east-1
      - AWS_SHARED_CREDENTIALS_FILE=/etc/s3pop/aws_credentials
    volumes:
      - ./aws-config:/home/s3pop/.aws/config:ro
      - ./s3pop-state:/home/s3pop/.email
      - ./my_aws_credentials:/etc/s3pop/aws_credentials:ro
```

**Cloudflare R2**
```yaml
version: '3.8'

services:
  s3pop-server:
    image: ghcr.io/arran4/s3pop-server:latest
    ports:
      - "127.0.0.1:5110:5110"
    environment:
      - S3POP_S3_BUCKET=my-email-bucket
      - S3POP_S3_ENDPOINT=https://<ACCOUNT_ID>.r2.cloudflarestorage.com
      - S3POP_S3_FORCE_PATH_STYLE=true
      - AWS_REGION=auto
      - AWS_SHARED_CREDENTIALS_FILE=/etc/s3pop/aws_credentials
    volumes:
      - ./aws-config:/home/s3pop/.aws/config:ro
      - ./s3pop-state:/home/s3pop/.email
      - ./my_r2_credentials:/etc/s3pop/aws_credentials:ro
```


#### Docker Compose Examples (with Docker Secrets)

Docker Compose supports mounting credentials as secrets instead of storing them in environment variables. Restrict access to the source files; file-backed Compose secrets do not themselves encrypt those files.

**Standard AWS S3**
```yaml
version: '3.8'

services:
  s3pop-server:
    image: ghcr.io/arran4/s3pop-server:latest
    ports:
      - "127.0.0.1:5110:5110"
    environment:
      - S3POP_S3_BUCKET=my-email-bucket
      - AWS_REGION=us-east-1
      - AWS_SHARED_CREDENTIALS_FILE=/run/secrets/aws_credentials
    volumes:
      - ./aws-config:/home/s3pop/.aws/config:ro
      - ./s3pop-state:/home/s3pop/.email
    secrets:
      - source: aws_credentials
        target: aws_credentials

secrets:
  aws_credentials:
    file: ./my_aws_credentials
```

**Cloudflare R2**
```yaml
version: '3.8'

services:
  s3pop-server:
    image: ghcr.io/arran4/s3pop-server:latest
    ports:
      - "127.0.0.1:5110:5110"
    environment:
      - S3POP_S3_BUCKET=my-email-bucket
      - S3POP_S3_ENDPOINT=https://<ACCOUNT_ID>.r2.cloudflarestorage.com
      - S3POP_S3_FORCE_PATH_STYLE=true
      - AWS_REGION=auto
      - AWS_SHARED_CREDENTIALS_FILE=/run/secrets/aws_credentials
    volumes:
      - ./aws-config:/home/s3pop/.aws/config:ro
      - ./s3pop-state:/home/s3pop/.email
    secrets:
      - source: aws_credentials
        target: aws_credentials

secrets:
  aws_credentials:
    file: ./my_r2_credentials
```

### Client Configuration
Your client needs to be able to be setup to use separate user names and password for both the POP3 connection and the SMTP server, the app has been tested with Thunderbird and the Windows 10 mail client. 

Configure the pop server to have host 127.0.0.1 with the same port as you set in the pop3 config. (If you cant set the port for your client you may need to change the config for the server to match what the client expects, this will usually be port 110).

Use the mailbox key prefix without a trailing slash as the POP3 username (for example, `alice@example.com` for objects under `alice@example.com/`). Use a disposable dummy password if your client requires one; it is not validated. See the [storage contract and limitations](docs/receiving-email.md#mailbox-storage-contract) before choosing prefixes.

For SMTP (sending) configuration, please refer to your email provider's documentation.




# Todo in future
- Enable to run as a service/ daemon
- Improve and integration-test the receiving and storage guide
- Multiple client support
- IMAP Support?