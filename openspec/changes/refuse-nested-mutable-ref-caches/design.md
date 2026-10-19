## Context

Mutable acquisition derives a ref's cache directory by joining the registry hash directory and the ref, then clones on a miss or fetches and force-checks-out on a hit. Git locates the repository by walking up from the working directory, so every Git operation acts on the nearest enclosing repository, not necessarily one created for that ref.

## Decisions

- Before any Git operation, walk from the cache directory's parent up to, but excluding, the registry hash directory. An entry named `.git` at any of those levels means the ref would live inside another ref's checkout; refuse.
- On a cache hit, require the cache directory's own `.git` to be a real directory, which is what a clone creates. Its absence means Git would discover an enclosing repository; refuse.
- Errors name the ref, its cache directory and, for nesting, the enclosing checkout. No diagnostic code is introduced; mutable acquisition errors are uncoded today.
- Use `Lstat`, so a case-insensitive filesystem detects `Foo` versus `foo/x` nesting through the filesystem itself.

## Alternatives Considered

- Encoding each ref as a single path component: removes the conflict entirely, but changes the cache layout that catalog keys, garbage collection, status freshness and existing caches depend on, and needs migration or dual-layout tolerance.
- Comparing `git rev-parse --show-toplevel` with the cache directory: catches the same cases but spawns Git per acquisition and still runs Git inside the foreign tree before refusing.
- Treating a non-checkout directory as a cache miss and re-cloning: cloning into a non-empty directory fails, and deleting it would delete another ref's checkout.
