-- Local schema reconstructed from the pinned upstream repository queries.
CREATE TABLE device_account (
    id uuid PRIMARY KEY,
    mdvm_wi_id uuid NOT NULL UNIQUE,
    wi_mdvm_auth_pubk_der bytea NOT NULL UNIQUE,
    device_type varchar(16) NOT NULL,
    device_class jsonb NOT NULL,
    android_attestation_details jsonb,
    ios_devicecheck_attestation jsonb,
    ios_devicecheck_assertion jsonb,
    wi_handle text,
    revoked_at timestamptz,
    updated_at timestamptz NOT NULL,
    version bigint NOT NULL DEFAULT 0
);
CREATE INDEX device_account_handle ON device_account (wi_handle);
CREATE TABLE wpb_account (
    id uuid PRIMARY KEY,
    wpb_account_id uuid NOT NULL UNIQUE,
    wi_mdvm_auth_pubk_der bytea NOT NULL UNIQUE,
    wpb_wi_revocation_hash bytea UNIQUE,
    wi_handle text,
    revoked_at timestamptz,
    mdvm_wi_id uuid
);
CREATE INDEX wpb_account_handle ON wpb_account (wi_handle);
CREATE TABLE rwsca_account (
    id uuid PRIMARY KEY,
    rwsca_account_id uuid NOT NULL UNIQUE,
    wi_mdvm_auth_pubk_der bytea NOT NULL UNIQUE,
    wi_rwsca_pin_pubk_der bytea,
    rwsca_pin_try_counter integer,
    rwsca_pin_try_failed_at timestamptz,
    wi_handle text,
    revoked_at timestamptz,
    mdvm_wi_id uuid
);
CREATE INDEX rwsca_account_handle ON rwsca_account (wi_handle);
CREATE TABLE notification_registration (
    id uuid PRIMARY KEY,
    account_id uuid NOT NULL UNIQUE,
    mpp_registration_token text NOT NULL,
    registered_at timestamptz NOT NULL
);
CREATE TABLE status_list (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    pool text NOT NULL,
    bits_per_entry integer NOT NULL,
    size integer NOT NULL,
    cursor integer NOT NULL DEFAULT 0,
    seed bytea NOT NULL,
    data bytea NOT NULL,
    version integer NOT NULL DEFAULT 0,
    created timestamptz NOT NULL DEFAULT now(),
    exhausted_at timestamptz,
    CHECK (cursor BETWEEN 0 AND size)
);
CREATE UNIQUE INDEX status_list_current ON status_list (pool) WHERE cursor < size;
CREATE TABLE status_list_entry (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    account_id uuid NOT NULL,
    list_id uuid NOT NULL REFERENCES status_list(id) ON DELETE CASCADE,
    idx integer NOT NULL,
    exp timestamptz NOT NULL,
    created timestamptz NOT NULL DEFAULT now(),
    client_instance_id uuid NOT NULL UNIQUE,
    UNIQUE (list_id, idx)
);
CREATE INDEX status_list_entry_account ON status_list_entry (account_id, exp);
CREATE INDEX status_list_entry_expiry ON status_list_entry (exp);
CREATE TABLE mdvm_analytics (
    id uuid PRIMARY KEY,
    operation_name text NOT NULL,
    auth_challenge text NOT NULL,
    skip_integrity_checks text NOT NULL,
    request jsonb NOT NULL,
    exception_details jsonb,
    version bigint NOT NULL DEFAULT 0
);
