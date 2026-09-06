pluginManagement {
    repositories {
        gradlePluginPortal()
        mavenCentral()
    }
}
rootProject.name = "german-wallet-local-backend"

val upstreamDir = "upstream/backend"
require(file("$upstreamDir/gradle/libs.versions.toml").isFile) {
    "Initialize the backend source first: git submodule update --init --recursive"
}
dependencyResolutionManagement {
    repositories { mavenCentral() }
    versionCatalogs {
        create("upstream") { from(files("$upstreamDir/gradle/libs.versions.toml")) }
    }
}
