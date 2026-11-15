// Compile the exact proposed rlookup files against their real pinned native API.
// This is not the full Search workspace build or a Redis module binary.
#[path = "../../source_hash.rs"]
pub mod source_hash;
#[path = "../../source_hash_native.rs"]
pub mod source_hash_native;
