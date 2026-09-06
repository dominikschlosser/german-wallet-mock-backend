#!/usr/bin/env bash
set -euo pipefail
umask 077

: "${MOCK_PUBLIC_URL:?MOCK_PUBLIC_URL is required}"
: "${S3_ENDPOINT:?S3_ENDPOINT is required}"

state=/data
pin=123456
token=wallet-local
marker="$state/provisioned.json"

if [[ -f "$marker" ]]; then
    if ! jq -e --arg url "$MOCK_PUBLIC_URL" '.public_url == $url' "$marker" >/dev/null; then
        echo "Public origin changed: choose a separate Compose project and fresh volumes" >&2
        exit 1
    fi
    exit 0
fi
if [[ -e "$state/tokens" ]]; then
    echo "Incomplete provisioning found. Inspect this test volume before retrying" >&2
    exit 1
fi

mkdir -p "$state"
mkdir -m 700 "$state/tokens"
cat >"$state/softhsm2.conf" <<'EOF'
directories.tokendir = /data/tokens
objectstore.backend = file
log.level = ERROR
EOF
export SOFTHSM2_CONF="$state/softhsm2.conf"
softhsm2-util --init-token --free --label "$token" --so-pin "$pin" --pin "$pin"
pkcs11=(pkcs11-tool --module /usr/lib/softhsm/libsofthsm2.so --token-label "$token" --login --pin "$pin")

# Temporary private keys stay in container memory until they are imported.
work_dir=$(mktemp -d /dev/shm/wallet-provision.XXXXXXXX)
trap 'rm -rf -- "$work_dir"' EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

openssl genpkey -algorithm EC -pkeyopt ec_paramgen_curve:P-256 -out "$work_dir/ca.key"
openssl req -new -x509 -sha256 -days 3650 -key "$work_dir/ca.key" \
    -subj '/CN=German Wallet Local CA' -out "$state/ca.pem" \
    -addext 'basicConstraints=critical,CA:TRUE,pathlen:0' \
    -addext 'keyUsage=critical,digitalSignature,keyCertSign,cRLSign'

s3=(curl --silent --show-error --connect-timeout 10 --max-time 60
    --aws-sigv4 aws:amz:us-east-1:s3 --user local-wallet:local-wallet-password)
bucket="${S3_ENDPOINT%/}/wallet-certificates"
bucket_status=$("${s3[@]}" --head --output /dev/null --write-out '%{http_code}' "$bucket")
case "$bucket_status" in
200) ;;
404) "${s3[@]}" --fail --request PUT "$bucket" ;;
*)
    echo "Cannot access certificate bucket: HTTP $bucket_status" >&2
    exit 1
    ;;
esac

cat >"$work_dir/signer.ext" <<'EOF'
basicConstraints=critical,CA:FALSE
keyUsage=critical,digitalSignature
EOF
index=1
for name in mdvm wpb wte status; do
    printf -v id '%02x' "$index"
    openssl genpkey -algorithm EC -pkeyopt ec_paramgen_curve:P-256 -out "$work_dir/signer.key"
    openssl pkey -in "$work_dir/signer.key" -outform DER -out "$work_dir/private.der"
    openssl pkey -in "$work_dir/signer.key" -pubout -outform DER -out "$work_dir/public.der"
    "${pkcs11[@]}" --write-object "$work_dir/private.der" --type privkey \
        --id "$id" --label "$name-local-prvk" --usage-sign --private --sensitive
    "${pkcs11[@]}" --write-object "$work_dir/public.der" --type pubkey \
        --id "$id" --label "$name-local-pubk" --usage-sign
    openssl req -new -key "$work_dir/signer.key" -subj "/CN=$name local signer" -out "$work_dir/signer.csr"
    openssl x509 -req -in "$work_dir/signer.csr" -CA "$state/ca.pem" -CAkey "$work_dir/ca.key" \
        -set_serial "0x$(openssl rand -hex 16)" -days 1825 -sha256 \
        -extfile "$work_dir/signer.ext" -out "$work_dir/signer.pem"
    cat "$work_dir/signer.pem" "$state/ca.pem" >"$work_dir/chain.pem"
    "${s3[@]}" --fail --upload-file "$work_dir/chain.pem" "$bucket/$token/$name-local.pem"
    index=$((index + 1))
done

index=20
for name in challenge pin aead master; do
    printf -v id '%02x' "$index"
    if [[ "$name" == challenge || "$name" == pin ]]; then
        usage=(--key-type GENERIC:32 --usage-sign)
    else
        usage=(--key-type AES:32 --usage-decrypt --usage-wrap)
    fi
    "${pkcs11[@]}" --keygen "${usage[@]}" --id "$id" --label "$name-local" --private --sensitive
    index=$((index + 1))
done

jq -n --arg url "$MOCK_PUBLIC_URL" '{version: 1, public_url: $url}' >"$state/provisioned.json.tmp"
mv "$state/provisioned.json.tmp" "$marker"
echo "Provisioned local SoftHSM keys and certificate chains"
