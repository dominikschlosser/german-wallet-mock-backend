# Compatibility 

The original source comes from the
[German National Wallet backend on GitHub](https://github.com/german-national-wallet/de-eudi-wallet-backend).
It is included as the [`upstream/backend`](../upstream/backend) Git submodule.
This repository records the backend commit to use. Run
`git -C upstream/backend rev-parse HEAD` to see the version in your checkout.

The build compiles every original Kotlin file under
`upstream/backend/src/main/kotlin` and starts
`de.eudiwallet.backend.WalletBackendApplicationKt`. The wallet endpoints, account
logic, database queries, request signatures and token formats come from that
code.

MDVM handles device registration and verification. WPB issues wallet instance
attestations and handles revocation. RWSCA manages PIN sessions, creates wallet
keys and signs data. PNS stores push registrations. The original code also
allocates, serves and updates status lists.

## Build files and configuration

The public repository includes a dependency catalog but no build files. This
project supplies a Gradle build that reads that catalog. It allows newer
dependencies required by Makoto and OpenTelemetry to take precedence over the
versions supplied by Spring Boot.

The repository also lacks generated constants used in OpenAPI annotations.
Local files provide descriptions and empty examples for those annotations.
They do not change how requests are handled. Remove those files if the original
repository starts providing the constants itself.

Local configuration enables all services in one process and points them at the
local database and certificate storage. The build also supplies the Git version
information expected by the backend's response headers.

## Database

The original repository queries run against PostgreSQL. The Flyway migration in
this project creates the seven tables and their indexes from the published
queries. The production database migrations were not included in the mirror.

When a change needs new tables or columns, add a migration so existing local
databases can be upgraded.

## Keys and certificates

SoftHSM stores the keys in software. The original PKCS#11 code uses it to sign,
wrap keys, encrypt and verify data. On the first start, the setup creates the
signing keys, HMAC and AES keys, and a local CA.

The Bash setup script uses OpenSSL and the SoftHSM tools to create these keys
and certificates. Temporary private key files stay in container memory and are
removed when setup finishes.

MinIO stores the certificate chains. The original certificate loader downloads
them through S3 and checks that each signing certificate matches its key.

Keys and certificates are kept across restarts. Setup stops with an error if an
earlier initialization was incomplete or the public address has changed. It
does not replace existing keys in either case.

Automatic certificate renewal and hardware failures are not covered by this
setup. The WTE tokens contain the original claims about key storage and user
authentication, but the local keys have no hardware protection.

## Revocation and push notifications

A local publisher replaces Kafka delivery. It calls the original WPB, RWSCA and
MDVM revocation services before returning. This makes revocation immediate for
local tests. It does not test Kafka retries, message ordering or outages.

The original PNS service stores push registrations. Kafka and push delivery are
disabled in the local configuration.

## Device integrity

The local configuration allows upstream's integrity checks to be skipped.
Clients must request this with `Skip-Integrity-Checks: true` or
`Skip-Integrity-Checks: all` on MDVM registration and renewal. The iOS DEV
simulator already sends the header. Requests without it use the original
integrity checks.

Other validation still runs, including HTTP signatures and iOS version checks.
The mobile app may also perform integrity checks before sending a request. Those
need to be configured in the app's debug build.

## Local endpoints

`/actuator/health` reports whether the backend is healthy.

The original `X-Service-Version` response header reports the backend revision.

`/mock/ca.pem` provides the CA certificate for your local PID issuer. Configure the
issuer to trust it before testing credential issuance. This certificate does not
configure HTTPS or make public PID providers trust the local backend.
