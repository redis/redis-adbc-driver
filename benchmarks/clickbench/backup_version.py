"""Verify immutable snapshot identity with the available S3 read permissions."""


def verify_snapshot_head(s3, bucket, record):
    expected_version = record['version_id']
    if not expected_version or expected_version == 'null':
        raise ValueError('A concrete snapshot version is required')
    method = 'explicit_version'
    try:
        head = s3.head_object(Bucket=bucket, Key=record['key'], VersionId=expected_version)
    except Exception as error:
        code = getattr(error, 'response', {}).get('Error', {}).get('Code')
        if code not in ('403', 'AccessDenied'):
            raise
        # GetObject can permit HEAD of the current version even when
        # GetObjectVersion is absent. Never accept a different current version.
        head = s3.head_object(Bucket=bucket, Key=record['key'])
        method = 'current_head_exact_version_match'
    if (head.get('VersionId') != expected_version
            or head.get('ContentLength') != record['bytes']
            or head.get('Metadata', {}).get('sha256') != record['sha256']):
        raise ValueError('Snapshot version, size or SHA256 metadata differs')
    return {'method': method, 'version_id': expected_version,
            'bytes': head['ContentLength'], 'sha256': head['Metadata']['sha256']}
