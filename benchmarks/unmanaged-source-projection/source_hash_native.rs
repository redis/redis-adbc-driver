/*
 * Copyright (c) 2006-Present, Redis Ltd.
 * All rights reserved.
 *
 * Licensed under your choice of the Redis Source Available License 2.0
 * (RSALv2); or (b) the Server Side Public License v1 (SSPLv1); or (c) the
 * GNU Affero General Public License v3 (AGPLv3).
 */

//! Native Redis module adapter for the shared bounded source-projection core.
//! No query grammar, coordinator, cursor, or caller ACL implementation is wired
//! here. In particular, RedisModule_OpenKey is not an authorization check.

use crate::source_hash::{
    SourceHashBackend, SourceHashError, SourceHashKind, SourceHashLimits, SourceHashProjection,
    project_source_hash,
};
use redis_module::{
    KeyType, RedisError, RedisString,
    key::{KeyFlags, RedisKey},
};
use std::ptr::NonNull;

struct NativeHash<'a, A, C> {
    ctx: NonNull<redis_module::RedisModuleCtx>,
    name: &'a RedisString,
    fields: &'a [RedisString],
    key: Option<RedisKey>,
    authorize: A,
    checkpoint: C,
}

impl<A, C> SourceHashBackend for NativeHash<'_, A, C>
where
    A: FnMut(&RedisString) -> Result<(), RedisError>,
    C: FnMut() -> Result<(), RedisError>,
{
    type Value = RedisString;
    type Error = RedisError;

    fn key_name_bytes(&self) -> usize {
        self.name.as_slice().len()
    }
    fn field_count(&self) -> usize {
        self.fields.len()
    }
    fn field_name_bytes(&self, index: usize) -> usize {
        self.fields[index].as_slice().len()
    }
    fn authorize(&mut self) -> Result<(), Self::Error> {
        (self.authorize)(self.name)
    }
    fn checkpoint(&mut self) -> Result<(), Self::Error> {
        (self.checkpoint)()
    }

    fn open(&mut self) -> Result<SourceHashKind, Self::Error> {
        // Empty extra flags retain native expiry behavior. Do not use the
        // index loader's ACCESS_EXPIRED/NOEXPIRE flags for live source reads.
        let key = RedisKey::open_with_flags(self.ctx.as_ptr(), self.name, KeyFlags::empty());
        let kind = if key.is_null() || key.key_type() == KeyType::Empty {
            SourceHashKind::Missing
        } else if key.key_type() == KeyType::Hash {
            SourceHashKind::Hash
        } else {
            SourceHashKind::WrongType
        };
        self.key = Some(key);
        Ok(kind)
    }

    fn get(&mut self, index: usize) -> Result<Option<Self::Value>, Self::Error> {
        // Both names and values are length-aware RedisStrings. There is no
        // C-string truncation, schema lookup, sort-vector access or f64 step.
        self.key
            .as_ref()
            .expect("source HASH opened before reads")
            .hash_get_by_string(&self.fields[index])
    }
    fn value_bytes(&self, value: &Self::Value) -> usize {
        value.as_slice().len()
    }
}

/// Project authoritative source fields using one native HASH handle.
///
/// `authorize` must check the current query user's source-key read permission,
/// not only index-prefix membership; callers must never pass an allow-all gate
/// for untrusted queries. `checkpoint` must enforce query cancellation/budgets
/// without yielding the Redis module execution lock or executing reentrant
/// keyspace mutations. Neither callback is cached.
/// The owned RedisStrings in a successful result outlive the open key handle.
///
/// This low-level prototype is not callable through a public Search command.
/// Query planning, warning/error propagation, distributed serialization and
/// cursor integration remain separate prerequisites before driver adoption.
///
/// # Safety
///
/// `ctx` must reference a valid live Redis module context on its owning thread.
/// The caller must hold the Redis module execution lock for the entire call;
/// neither callback may release it or invalidate/mutate an open key handle.
/// Context lifetime must cover this call and destruction of returned RedisStrings.
pub unsafe fn project_native_source_hash<A, C>(
    ctx: NonNull<redis_module::RedisModuleCtx>,
    name: &RedisString,
    fields: &[RedisString],
    limits: SourceHashLimits,
    authorize: A,
    checkpoint: C,
) -> Result<SourceHashProjection<RedisString>, SourceHashError<RedisError>>
where
    A: FnMut(&RedisString) -> Result<(), RedisError>,
    C: FnMut() -> Result<(), RedisError>,
{
    let mut native = NativeHash {
        ctx,
        name,
        fields,
        key: None,
        authorize,
        checkpoint,
    };
    project_source_hash(&mut native, limits)
}
