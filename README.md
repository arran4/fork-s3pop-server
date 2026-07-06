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

## Solution Details
You will need
 - An AWS Account (aws.amazon.com)
 - Your own registered domain. This can be with AWS or another provider, though it will be easier if you use AWS name servers (even if your registrar is someone else). I recommend [VentraIP] (https://ventraip.com.au) for getting a domain if you are in Australia.

In these instructions I will assume you have setup an AWS account and purchased a domain from the registrar of your choice. There are several steps to implementing this solution. Initial setup is a little complex however once it is setup you will have a cheap and easily maintainable solution.

At present the instructions assume some familiarity with AWS usage and configuring email clients. Expanding the documentation is one of the current todo items for this project.
### Server Configuration
#### Bucket configuration
 - Create an s3 bucket for storing your emails something like mail.mydomainname.com is a good thing to name it.
 - Create an IAM Role using AmazonS3ReadOnlyAccess (edit to be specific to your mail bucket for extra security).
 - Create a new IAM user with Access Key and Secret key that uses your new role.
 - Put these new credentials in your credentials file in the .aws folder in your home directory. Set you aws-region in your config file in this same directory to be the same as your bucket region (ap-southeast-2 if you are in Sydney)
 #### DNS Setup (Optional, only if you are using route53 for DNS)
  - Set up your domain to use AWS nameservers if it does not already-  
 #### Email Sending (SMTP Setup)
 - Email sending is handled outside of this server by your chosen provider (e.g., AWS SES, Mailgun, SendGrid, etc.). Configure your email client to use their SMTP settings.
 #### Email Receiving (Server Setup)
 - Set up a rule set to deliver emails sent to your desired email address to your s3 bucket.
 #### POP3 Server Config
 - Download the zip in releases and unzip into a directory you want to keep it
 - Edit your server-config.json (in the root directory of your install), set the bucket name to the name of your bucket. Note the port (or choose your own) for use in configuring your client.
 - If you are using an S3-compatible service like Cloudflare R2, you can also set `s3Endpoint` to your custom endpoint URL and `s3ForcePathStyle` to true if required.
 - Optionally set the program to start when your os starts

### Docker
Alternatively, you can use the official Docker image. You can pull the latest image with:
```bash
docker pull ghcr.io/arran4/s3pop-server:latest
```

Configuration can be passed via environment variables instead of (or overriding) the JSON configuration file:

| Environment Variable | Description | Example |
|---|---|---|
| `S3POP_PORT` | The port the POP3 server will listen on (default 5110). | `110` |
| `S3POP_S3_BUCKET` | The name of the S3 bucket to read emails from. | `my-email-bucket` |
| `S3POP_S3_ENDPOINT` | Custom S3 endpoint URL (e.g. for Cloudflare R2). | `https://account-id.r2.cloudflarestorage.com` |
| `S3POP_S3_FORCE_PATH_STYLE` | Force path style URLs for S3 operations (`true`/`false`). | `true` |
| `S3POP_CONFIG` | Path to a custom JSON configuration file. | `/etc/s3pop/config.json` |

#### Docker Run Examples

**Standard AWS S3**
```bash
docker run -d --rm --name s3pop-server -p 5110:5110 \
  -e S3POP_S3_BUCKET="my-email-bucket" \
  -e AWS_ACCESS_KEY_ID="your_access_key" \
  -e AWS_SECRET_ACCESS_KEY="your_secret_key" \
  -e AWS_REGION="us-east-1" \
  ghcr.io/arran4/s3pop-server:latest
```

**Cloudflare R2**
```bash
docker run -d --rm --name s3pop-server -p 5110:5110 \
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
docker run -d --rm --name s3pop-server -p 5110:5110 \
  -e S3POP_S3_BUCKET="my-email-bucket" \
  -e AWS_REGION="us-east-1" \
  -e AWS_SHARED_CREDENTIALS_FILE="/run/secrets/aws_credentials" \
  -v ./my_aws_credentials:/run/secrets/aws_credentials:ro \
  ghcr.io/arran4/s3pop-server:latest
```

**Cloudflare R2**
```bash
docker run -d --rm --name s3pop-server -p 5110:5110 \
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
      - "5110:5110"
    environment:
      - S3POP_S3_BUCKET=my-email-bucket
      - AWS_REGION=us-east-1
      - AWS_SHARED_CREDENTIALS_FILE=/etc/s3pop/aws_credentials
    volumes:
      - ./my_aws_credentials:/etc/s3pop/aws_credentials:ro
```

**Cloudflare R2**
```yaml
version: '3.8'

services:
  s3pop-server:
    image: ghcr.io/arran4/s3pop-server:latest
    ports:
      - "5110:5110"
    environment:
      - S3POP_S3_BUCKET=my-email-bucket
      - S3POP_S3_ENDPOINT=https://<ACCOUNT_ID>.r2.cloudflarestorage.com
      - S3POP_S3_FORCE_PATH_STYLE=true
      - AWS_REGION=auto
      - AWS_SHARED_CREDENTIALS_FILE=/etc/s3pop/aws_credentials
    volumes:
      - ./my_r2_credentials:/etc/s3pop/aws_credentials:ro
```


#### Docker Compose Examples (with Docker Secrets)

Docker compose has native support for secrets which is the most secure way to pass credentials to containers.

**Standard AWS S3**
```yaml
version: '3.8'

services:
  s3pop-server:
    image: ghcr.io/arran4/s3pop-server:latest
    ports:
      - "5110:5110"
    environment:
      - S3POP_S3_BUCKET=my-email-bucket
      - AWS_REGION=us-east-1
      - AWS_SHARED_CREDENTIALS_FILE=/run/secrets/aws_credentials
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
      - "5110:5110"
    environment:
      - S3POP_S3_BUCKET=my-email-bucket
      - S3POP_S3_ENDPOINT=https://<ACCOUNT_ID>.r2.cloudflarestorage.com
      - S3POP_S3_FORCE_PATH_STYLE=true
      - AWS_REGION=auto
      - AWS_SHARED_CREDENTIALS_FILE=/run/secrets/aws_credentials
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

The username you use to connect to the POP3 server should be the key prefix (folder) you use in your S3 bucket to store email.

For SMTP (sending) configuration, please refer to your email provider's documentation.




# Todo in future
- Enable to run as a service/ daemon
- Better Docs
- Multiple client support
- IMAP Support?