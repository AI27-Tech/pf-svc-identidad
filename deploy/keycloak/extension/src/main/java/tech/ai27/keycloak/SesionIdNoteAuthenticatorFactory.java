package tech.ai27.keycloak;

import java.util.List;

import org.keycloak.Config;
import org.keycloak.authentication.Authenticator;
import org.keycloak.authentication.AuthenticatorFactory;
import org.keycloak.models.AuthenticationExecutionModel.Requirement;
import org.keycloak.models.KeycloakSession;
import org.keycloak.models.KeycloakSessionFactory;
import org.keycloak.provider.ProviderConfigProperty;

/**
 * SesionIdNoteAuthenticatorFactory registra el authenticator
 * {@link SesionIdNoteAuthenticator} en Keycloak (DEC-083).
 *
 * <p>El {@link #getId() id} es {@code ai27-sesion-id-note}: debe coincidir
 * EXACTAMENTE con el {@code authenticator} que el realm referencia en el flujo
 * {@code ai27-direct-grant} (ver deploy/keycloak/realm-ai27.json).
 */
public class SesionIdNoteAuthenticatorFactory implements AuthenticatorFactory {

    /** Id del proveedor; lo referencia el flujo del realm. */
    public static final String PROVIDER_ID = "ai27-sesion-id-note";

    // El authenticator no tiene estado, así que se comparte una sola instancia.
    private static final SesionIdNoteAuthenticator INSTANCIA = new SesionIdNoteAuthenticator();

    private static final Requirement[] REQUIREMENT_CHOICES = {
            Requirement.REQUIRED,
            Requirement.DISABLED,
    };

    @Override
    public String getId() {
        return PROVIDER_ID;
    }

    @Override
    public Authenticator create(KeycloakSession session) {
        return INSTANCIA;
    }

    @Override
    public String getDisplayType() {
        return "AI27: nota de sesion sesion_id";
    }

    @Override
    public String getReferenceCategory() {
        return "AI27";
    }

    @Override
    public boolean isConfigurable() {
        return false;
    }

    @Override
    public Requirement[] getRequirementChoices() {
        return REQUIREMENT_CHOICES;
    }

    @Override
    public boolean isUserSetupAllowed() {
        return false;
    }

    @Override
    public String getHelpText() {
        return "Guarda el parametro de formulario sesion_id como nota de sesion SESION_ID "
                + "para que el note-mapper lo emita como claim sesion_id (DEC-083).";
    }

    @Override
    public List<ProviderConfigProperty> getConfigProperties() {
        return List.of();
    }

    @Override
    public void init(Config.Scope config) {
        // Sin configuración de arranque.
    }

    @Override
    public void postInit(KeycloakSessionFactory factory) {
        // Sin trabajo posterior a la inicialización.
    }

    @Override
    public void close() {
        // Sin recursos que liberar.
    }
}
