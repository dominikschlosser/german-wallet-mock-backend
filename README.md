# German wallet local backend

Run the German EUDI wallet backend on your own machine for testing with the iOS
and Android wallets.

The application uses the original Kotlin code from the
[German National Wallet backend](https://github.com/german-national-wallet/de-eudi-wallet-backend).
This repository adds the build files, configuration and local services needed to
run it. PostgreSQL stores accounts and status lists, SoftHSM handles cryptographic
keys, and MinIO stores certificates. Go is used for HTTP integration tests.

## Start the backend

You need Git and Docker with Compose v2 and BuildKit. Java and Gradle run in
Docker. Clone this repository with its backend submodule:

```sh
git clone --recurse-submodules https://github.com/dominikschlosser/german-wallet-mock-backend.git
cd german-wallet-mock-backend
```

If you already have a checkout, initialize the submodule:

```sh
git submodule update --init --recursive
```

Start the services:

```sh
bash docker/start.sh
```

This builds the application from `upstream/backend` and starts the services.
The first build takes longer because it downloads dependencies and Docker images.

Check that the server is running:

```sh
curl http://localhost:8080/actuator/health
```

The backend listens on localhost by default. Database contents, keys and
certificates are stored in Docker volumes, so they survive a restart.

```sh
docker compose stop
bash docker/start.sh
```

This setup is for local testing. Its passwords and token PIN are included in the
configuration, and it allows requests to skip Apple and Google integrity checks.
HTTP signatures and other request validation still apply.

### Use a different address or a fresh database

`MOCK_PUBLIC_URL` is the address written into tokens and status list links.
Both the wallet and the PID issuer must be able to reach it. For a phone on the
same network, use your computer's address:

```sh
MOCK_BIND_ADDRESS=0.0.0.0 MOCK_PUBLIC_URL=http://192.168.1.20:8080 bash docker/start.sh
```

The address is saved when the services first start. If you change it later, use a
new Compose project. A new project also gives you fresh accounts and keys:

```sh
COMPOSE_PROJECT_NAME=wallet-scenario-b MOCK_PORT=8081 \
  MOCK_PUBLIC_URL=http://localhost:8081 bash docker/start.sh
```

Use the same project name when stopping or restarting that setup. The server
uses HTTP. If you need HTTPS, put a TLS proxy in front of it.

## Connect a wallet

Change the wallet's backend host and keep its `/v1/...` request paths.
For registration and renewal without platform attestation, the client must send
`Skip-Integrity-Checks: true`. The iOS DEV simulator already does this. Configure
the Android debug build to skip integrity checks as well.

### iOS

Use the DEV build in the simulator. Set the debug Wallet Host URL to
`http://localhost:8080`, or include
[`config/ios/LocalMock.xcconfig`](config/ios/LocalMock.xcconfig) last in your
local DEV configuration. Restart the app after changing the address.

For HTTP, set `NSAppTransportSecurity.NSAllowsLocalNetworking = YES` in your debug
Info.plist. For HTTPS, trust the development TLS certificate in the simulator.

The DEV simulator build already skips platform attestation. A physical iPhone
still calls App Attest. It needs valid development entitlements, or a change to
the debug app that skips that call. Use your computer's network address instead
of localhost when testing on a phone.

### Android

Set `EnvironmentConfig.serverHostURL` in your local `EnvironmentConfigImpl` to
`http://localhost:8080/`. Forward the device's port to your computer:

```sh
adb reverse tcp:8080 tcp:8080
```

Use [`config/android/network_security_config.xml`](config/android/network_security_config.xml)
as the debug app's `res/xml/network_security_config.xml`. Reference it as
`@xml/network_security_config` in the debug manifest's
`android:networkSecurityConfig` attribute. Use the simulator build and configure
its integrity checks to be skipped. The Android emulator can also reach your
computer at `10.0.2.2`.

The Android checkout currently lacks its Gradle build files, resources and
`EnvironmentConfigImpl`. You need a complete checkout to build the app.

### PID issuer and other services

You still need a separate PID issuer to issue credentials and a verifier to test
presentations. Download the local CA certificate from `/mock/ca.pem`. Configure
your issuer to trust that CA and accept the client ID `local-german-wallet`.

This CA signs the certificates used for wallet attestations and status tokens.
It is separate from the certificate used for HTTPS. Public PID providers will
not automatically trust it.

Feature flags, analytics and external EAA services use their own app settings.
The backend stores push registration tokens but does not send notifications
through APNs or FCM.

## Update the original backend

The [`upstream/backend`](upstream/backend) submodule points to the
[original repository on GitHub](https://github.com/german-national-wallet/de-eudi-wallet-backend).
Git records the selected backend commit as part of this repository.

After pulling changes to this repository, check out its recorded backend version:

```sh
git submodule update --init --recursive
```

To try a newer backend version, fetch the original repository and check out the
commit or tag you want to use. Replace `REVISION` with that value:

```sh
git -C upstream/backend fetch origin
git -C upstream/backend checkout REVISION
git diff --submodule=log
```

Review the changes, especially configuration, database queries and dependencies.
Then test the new version:

```sh
./gradlew check
./gradlew backendTest
bash docker/start.sh
```

Include `upstream/backend` in your next commit to record the new version. Other
developers can then get it with `git submodule update --init --recursive`.

The build compiles the updated Kotlin code directly. Changes to database tables
or infrastructure interfaces may also need changes here. If the original
repository adds its missing documentation constants, remove the matching local
constants so they are not defined twice.

## Development and tests

For development on the host, install JDK 25, Go 1.25 or newer, and Bash.

```sh
./gradlew check         # Kotlin compilation, formatting, shell syntax and Go static checks
./gradlew backendTest   # HTTP tests against the application running in Docker
./gradlew format        # Format local Kotlin and Go code
./gradlew bootJar       # Build the application with the host JDK
```

The HTTP tests cover registration, renewal, deletion, attestations, remote
signing, revocation, invalid signatures, account access and PIN locking. They
also restart the backend and check that tokens, wrapped keys and PIN failure
counts still work as expected.

Each integration run uses its own `wallet-test-*` Compose project. The services
are stopped afterwards, but their volumes are kept so you can inspect failures.

These tests have passed against the backend. Testing with the apps on devices
and with a public PID issuer is still outstanding. See
[how the local setup differs](docs/compatibility.md) for more detail.
