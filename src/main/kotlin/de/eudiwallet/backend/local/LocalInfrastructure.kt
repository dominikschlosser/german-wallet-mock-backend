package de.eudiwallet.backend.local

import de.eudiwallet.backend.mdvm.MdvmAccountService
import de.eudiwallet.backend.rwsca.RwscaAccountService
import de.eudiwallet.backend.shared.messaging.WalletInstanceRevocationEvent
import de.eudiwallet.backend.shared.messaging.WalletInstanceRevocationPublisher
import de.eudiwallet.backend.wpb.WpbAccountService
import org.springframework.beans.factory.ObjectProvider
import org.springframework.context.annotation.Bean
import org.springframework.context.annotation.Configuration
import org.springframework.context.annotation.Profile
import org.springframework.core.io.FileSystemResource
import org.springframework.http.MediaType
import org.springframework.web.bind.annotation.GetMapping
import org.springframework.web.bind.annotation.RestController

/** Delivers revocation events to the original services in this process. */
@Configuration
@Profile("local")
class LocalInfrastructure {
    @Bean
    fun localRevocationPublisher(
        mdvm: ObjectProvider<MdvmAccountService>,
        wpb: ObjectProvider<WpbAccountService>,
        rwsca: ObjectProvider<RwscaAccountService>,
    ): WalletInstanceRevocationPublisher =
        object : WalletInstanceRevocationPublisher {
            override suspend fun publish(event: WalletInstanceRevocationEvent) {
                wpb.getObject().revokeByWiHandle(event.wiHandle)
                rwsca.getObject().revokeByWiHandle(event.wiHandle)
                mdvm.getObject().revokeByWiHandle(event.wiHandle)
            }
        }
}

@RestController
@Profile("local")
class LocalCertificate {
    @GetMapping("/mock/ca.pem", produces = [MediaType.TEXT_PLAIN_VALUE])
    fun certificate() = FileSystemResource("/data/ca.pem")
}
