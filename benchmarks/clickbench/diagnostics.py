"""Keep actionable worker errors without retaining connection credentials."""
import re
from urllib.parse import unquote, urlsplit


def error_details(error, uri=None):
    message = str(error)
    if uri:
        # Redact both the whole URI and credentials a lower-level client may
        # have decoded or printed separately. Do this before truncating.
        message = message.replace(uri, '[redacted connection URI]')
        parsed = urlsplit(uri)
        for secret in (parsed.username, parsed.password):
            if secret:
                message = message.replace(secret, '[redacted]')
                message = message.replace(unquote(secret), '[redacted]')
    message = re.sub(r'\b[a-zA-Z][a-zA-Z0-9+.-]*://[^\s\"\'<>]+',
                     '[redacted URL]', message)
    return {'error_type': type(error).__name__,
            'status': str(getattr(error, 'status_code', 'unknown')),
            'message': message[:1000]}
