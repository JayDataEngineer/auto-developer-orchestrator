// Composite Backend - routes operations by path prefix
// Similar to Deep Agents' CompositeBackend pattern
//
// Routes different path prefixes to different backend implementations:
//   /project/*    → FilesystemBackend (real project files)
//   /memory/*     → StoreBackend (persistent cross-session memory)
//   /state/*      → StateBackend (ephemeral in-memory state)
//
// Usage:
//   const backend = new CompositeBackend([
//     { prefix: "/project/", backend: new FilesystemBackend("/path/to/project") },
//     { prefix: "/memory/", backend: new StoreBackend(store) },
//     { prefix: "/", backend: new StateBackend() }, // catch-all
//   ]);

import * as fs from "fs/promises";
import * as path from "path";

// Backend interface
export interface Backend {
  ls(dirPath: string): Promise<string[]>;
  read(filePath: string): Promise<string>;
  write(filePath: string, content: string): Promise<void>;
  edit(filePath: string, oldStr: string, newStr: string): Promise<void>;
  grep(pattern: string, dirPath: string): Promise<{ file: string; line: number; match: string }[]>;
  glob(pattern: string, dirPath: string): Promise<string[]>;
}

interface BackendRoute {
  prefix: string;
  backend: Backend;
}

export class CompositeBackend implements Backend {
  private routes: BackendRoute[];

  constructor(routes: BackendRoute[]) {
    // Sort by prefix length (longest first) for most-specific match
    this.routes = [...routes].sort((a, b) => b.prefix.length - a.prefix.length);
  }

  // Find the backend for a given path
  private resolvePath(filePath: string): { backend: Backend; relativePath: string } {
    for (const route of this.routes) {
      if (filePath.startsWith(route.prefix)) {
        const relativePath = filePath.slice(route.prefix.length);
        return { backend: route.backend, relativePath };
      }
    }
    // Fallback to last route (catch-all)
    const fallback = this.routes[this.routes.length - 1];
    return { backend: fallback.backend, relativePath: filePath };
  }

  async ls(dirPath: string): Promise<string[]> {
    const { backend, relativePath } = this.resolvePath(dirPath);
    return backend.ls(relativePath);
  }

  async read(filePath: string): Promise<string> {
    const { backend, relativePath } = this.resolvePath(filePath);
    return backend.read(relativePath);
  }

  async write(filePath: string, content: string): Promise<void> {
    const { backend, relativePath } = this.resolvePath(filePath);
    return backend.write(relativePath, content);
  }

  async edit(filePath: string, oldStr: string, newStr: string): Promise<void> {
    const { backend, relativePath } = this.resolvePath(filePath);
    return backend.edit(relativePath, oldStr, newStr);
  }

  async grep(pattern: string, dirPath: string): Promise<{ file: string; line: number; match: string }[]> {
    const { backend, relativePath } = this.resolvePath(dirPath);
    const results = await backend.grep(pattern, relativePath);
    // Prefix file paths back with the route prefix
    const route = this.routes.find(r => r.backend === backend);
    const prefix = route?.prefix || "";
    return results.map(r => ({ ...r, file: prefix + r.file }));
  }

  async glob(pattern: string, dirPath: string): Promise<string[]> {
    const { backend, relativePath } = this.resolvePath(dirPath);
    const results = await backend.glob(pattern, relativePath);
    const route = this.routes.find(r => r.backend === backend);
    const prefix = route?.prefix || "";
    return results.map(r => prefix + r);
  }

  // Aggregate ls across all backends for root "/"
  async lsRoot(): Promise<{ [prefix: string]: string[] }> {
    const result: { [prefix: string]: string[] } = {};
    for (const route of this.routes) {
      try {
        result[route.prefix] = await route.backend.ls("");
      } catch {
        result[route.prefix] = [];
      }
    }
    return result;
  }

  // Aggregate grep across all backends
  async grepAll(pattern: string): Promise<{ file: string; line: number; match: string }[]> {
    const allResults: { file: string; line: number; match: string }[] = [];
    for (const route of this.routes) {
      try {
        const results = await route.backend.grep(pattern, "");
        const prefix = route.prefix;
        allResults.push(...results.map(r => ({ ...r, file: prefix + r.file })));
      } catch {
        // Skip backends that don't support grep
      }
    }
    return allResults;
  }
}
