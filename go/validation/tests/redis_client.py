"""Raw fixture introspection uses the same verified TLS roots as ADBC."""


def raw_options(db_kwargs):
    options = {"decode_responses": True, "protocol": 3}
    if ca := db_kwargs.get("adbc.redis.tls_ca_file"):
        options["ssl_ca_certs"] = ca
    return options
