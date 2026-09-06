plugins {
    alias(upstream.plugins.kotlin.jvm)
    alias(upstream.plugins.kotlin.spring)
    alias(upstream.plugins.serialization)
    alias(upstream.plugins.spring.boot)
    alias(upstream.plugins.ktlint)
}

group = "local.wallet"
version = "1.0.0"
kotlin {
    jvmToolchain(25)
    sourceSets.main {
        kotlin.srcDir("upstream/backend/src/main/kotlin")
    }
}

dependencies {
    // Native Gradle platforms preserve newer transitive requirements from
    // Makoto and OpenTelemetry rather than forcing Spring's older defaults.
    implementation(platform("org.springframework.boot:spring-boot-dependencies:${upstream.versions.spring.boot.get()}"))
    implementation(platform(upstream.aws.sdk.bom))
    implementation(platform(upstream.opentelemetry.instrumentation.bom))
    implementation(upstream.spring.boot.starter.webflux)
    implementation(upstream.spring.boot.starter.webclient)
    implementation(upstream.spring.boot.starter.actuator)
    implementation(upstream.spring.boot.starter.security)
    implementation(upstream.spring.boot.starter.data.r2dbc)
    implementation(upstream.spring.boot.starter.validation)
    implementation(upstream.spring.boot.starter.flyway)
    implementation(upstream.spring.boot.kafka)
    implementation(upstream.r2dbc.postgresql)
    implementation(upstream.postgresql)
    implementation(upstream.flyway.postgresql)
    implementation(upstream.kotlin.reflect)
    implementation(upstream.kotlinx.coroutines.reactor)
    implementation(upstream.kotlinx.serialization.json)
    implementation(upstream.kotlin.logging)
    implementation(upstream.nimbus.jose.jwt)
    implementation(upstream.bouncycastle.pkix.jdk18)
    implementation(upstream.ngengine.bech32)
    implementation(upstream.authlete.http.message.signatures)
    implementation(upstream.springdoc.openapi.starter.webflux.ui)
    implementation(upstream.vdurmont.semver4j)
    implementation(upstream.warden.makoto)
    implementation(upstream.aws.sdk.s3)
    implementation(upstream.aws.sdk.url.connection.client)
    implementation(upstream.google.auth.library.oauth2.http)
    implementation(upstream.opentelemetry.spring.boot.starter)
    implementation(upstream.opentelemetry.extension.kotlin)
    implementation(upstream.opentelemetry.samplers)
}

springBoot { mainClass = "de.eudiwallet.backend.WalletBackendApplicationKt" }
val upstreamRevision =
    providers.gradleProperty("upstreamRevision").orElse(
        providers
            .exec {
                commandLine("git", "-C", "upstream/backend", "rev-parse", "HEAD")
            }.standardOutput.asText
            .map { it.trim() },
    )
tasks.processResources {
    inputs.property("upstreamRevision", upstreamRevision)
    filesMatching("git.properties") {
        val commit = upstreamRevision.get()
        require(commit.matches(Regex("[0-9a-f]{40}"))) {
            "Set UPSTREAM_REVISION to the backend commit when building with Docker."
        }
        expand("commit" to commit, "shortCommit" to commit.take(7))
    }
    from("upstream/backend/LICENSE") {
        into("META-INF")
        rename { "UPSTREAM-LICENSE" }
    }
}
tasks.bootRun { jvmArgs("--enable-native-access=ALL-UNNAMED") }

val checkLocal =
    tasks.register("checkLocal") {
        doLast {
            val commands =
                listOf(
                    listOf("bash", "-n", "docker/start.sh"),
                    listOf("bash", "-n", "docker/provision.sh"),
                    listOf("bash", "-n", "tests/run.sh"),
                    listOf("sh", "-n", "docker/entrypoint.sh"),
                    listOf("go", "vet", "./tests"),
                )
            for (command in commands) {
                providers
                    .exec { commandLine(command) }
                    .result
                    .get()
                    .assertNormalExitValue()
            }
            val unformatted =
                providers
                    .exec { commandLine("gofmt", "-l", "tests") }
                    .standardOutput.asText
                    .get()
            check(unformatted.isBlank()) { "Run ./gradlew format to format Go sources:\n$unformatted" }
        }
    }
tasks.check { dependsOn(checkLocal) }
tasks.register<Exec>("format") {
    dependsOn("ktlintFormat")
    commandLine("gofmt", "-w", "tests")
}
tasks.register<Exec>("backendTest") {
    group = "verification"
    description = "Run HTTP tests against the backend in Docker."
    commandLine("bash", "tests/run.sh")
}
ktlint {
    filter {
        exclude {
            !it.file.toPath().startsWith(projectDir.resolve("src").toPath()) && it.file.parentFile != projectDir
        }
    }
}
