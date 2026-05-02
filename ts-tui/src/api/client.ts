import type {
  PromptRequest,
  ApprovalRequestBody,
  SSEEvent,
  SchedulerJob,
} from "./types";

export class ApiClient {
  private baseUrl: string;

  constructor(baseUrl: string) {
    this.baseUrl = baseUrl.replace(/\/$/, "");
  }

  private async request<T>(
    method: string,
    path: string,
    body?: unknown
  ): Promise<T> {
    const res = await fetch(`${this.baseUrl}${path}`, {
      method,
      headers: { "Content-Type": "application/json" },
      body: body ? JSON.stringify(body) : undefined,
    });
    if (!res.ok) {
      const text = await res.text();
      throw new Error(`HTTP ${res.status}: ${text.slice(0, 200)}`);
    }
    return res.json() as Promise<T>;
  }

  /** Stream SSE events from a POST request. Returns an async generator. */
  async *streamPrompt(
    req: PromptRequest
  ): AsyncGenerator<SSEEvent, void, void> {
    const res = await fetch(`${this.baseUrl}/api/pux/prompt`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(req),
    });

    if (!res.ok) {
      throw new Error(`HTTP ${res.status}: ${res.statusText}`);
    }

    const reader = res.body!.getReader();
    const decoder = new TextDecoder();
    let buffer = "";
    let eventType = "";
    let preamble = true;

    try {
      while (true) {
        const { done, value } = await reader.read();
        if (done) break;

        buffer += decoder.decode(value, { stream: true });

        // Skip HTTP preamble (content-type headers etc)
        if (preamble) {
          const headerEnd = buffer.indexOf("\n\n");
          if (headerEnd !== -1) {
            buffer = buffer.slice(headerEnd + 2);
            preamble = false;
          }
        }

        const lines = buffer.split("\n");
        buffer = lines.pop() || ""; // keep incomplete line

        for (const line of lines) {
          if (line === "") {
            eventType = "";
            continue;
          }
          if (line.startsWith("event:")) {
            eventType = line.slice(6).trim();
            continue;
          }
          if (line.startsWith("data:") && eventType) {
            const data = line.slice(5).trim();
            if (data) {
              try {
                const parsed = JSON.parse(data);
                yield { type: eventType as SSEEvent["type"], data: parsed };
              } catch {
                // skip unparseable data
              }
            }
          }
        }
      }
    } finally {
      reader.releaseLock();
    }
  }

  async approve(req: ApprovalRequestBody): Promise<void> {
    await this.request("POST", "/api/pux/respond", req);
  }

  // Conversation management
  async getHistory(project: string, agentId?: string): Promise<any[]> {
    const params = new URLSearchParams({ project });
    if (agentId) params.set("agentId", agentId);
    try {
      const res = await fetch(`${this.baseUrl}/api/pux/history?${params}`);
      if (!res.ok) return [];
      const data = await res.json();
      return data as any[];
    } catch {
      return [];
    }
  }

  async deleteConversation(project: string, agentId: string): Promise<void> {
    const params = new URLSearchParams({ project, agentId });
    await fetch(`${this.baseUrl}/api/pux/conversation?${params}`, { method: "DELETE" });
  }

  // Artifacts
  async getArtifacts(agentId: string): Promise<any[]> {
    try {
      const res = await fetch(`${this.baseUrl}/api/pux/artifacts?agentId=${agentId}`);
      if (!res.ok) return [];
      return (await res.json()) as any[];
    } catch {
      return [];
    }
  }

  async getArtifact(id: string): Promise<any> {
    return this.request("GET", `/api/pux/artifacts/${id}`);
  }

  async renameConversation(project: string, agentId: string, title: string): Promise<void> {
    await this.request("PUT", "/api/pux/conversation/rename", { project, agentId, title });
  }
  async getJobs(): Promise<SchedulerJob[]> {
    const result = await this.request<{ jobs: SchedulerJob[] }>(
      "GET",
      "/api/scheduler/"
    );
    return result.jobs;
  }

  async createJob(
    req: Partial<SchedulerJob>
  ): Promise<{ id: string; name: string }> {
    return this.request("POST", "/api/scheduler/", req);
  }

  async updateJob(
    id: string,
    req: Partial<SchedulerJob>
  ): Promise<{ id: string; name: string }> {
    return this.request("PUT", `/api/scheduler/${id}`, req);
  }

  async deleteJob(id: string): Promise<void> {
    await this.request("DELETE", `/api/scheduler/${id}`);
  }

  async triggerJob(id: string): Promise<void> {
    await fetch(`${this.baseUrl}/api/scheduler/${id}/trigger`, {
      method: "POST",
    });
  }
}
