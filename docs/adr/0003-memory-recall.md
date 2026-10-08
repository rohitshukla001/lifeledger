# ADR 0003: Memory recall with embeddings and keyword match

- Status: Accepted
- Date: 2026-10-08

## Context

LifeLedger must remember facts about the user across sessions and find them again when a question uses different words. For example, "vehicle policy" must find "car insurance". One person has a few hundred to a few thousand memories. The user must be able to see, edit, and delete each memory.

## Decision

1. Each memory gets an embedding from the Token Factory embeddings API. The default model is `Qwen/Qwen3-Embedding-8B`. The Token Factory catalog has no NVIDIA embedding model. Nemotron models do all text generation.
2. The embedding is stored in the `memories` table as a little-endian float32 BLOB, with the model name. A vector from a different model counts as missing.
3. Recall loads all memories and calculates the score in Go. No vector index is necessary at this data volume.
4. The score is `0.75 × cosine similarity + 0.25 × keyword match`. The keyword match is the fraction of query words (without stop words) that occur in the memory. A memory with a score below 0.2 is not returned.
5. The query gets the Qwen3 retrieval instruction as a prefix. Stored memories are embedded without a prefix.
6. When the embedding call fails, LifeLedger still saves the memory, and recall uses the keyword match only. `lifeledger memory reindex` embeds the memories that have no current embedding.
7. A change to the text of a memory removes its old embedding in the same SQL statement. LifeLedger then makes a new embedding.
8. A new memory with the same normalized text as an existing memory is not saved again.

## Consequences

- Recall works without network access, with lower quality.
- A change of embedding model needs `lifeledger memory reindex`. Until then, recall uses the keyword match for the memories that are not reindexed.
- Each recall reads all embeddings. At 4096 dimensions, 5,000 memories use about 80 MB of reads for each recall. Above that size, a vector index is necessary.
- The weights and the threshold are heuristics. They need tuning with real data.
