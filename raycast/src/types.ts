export interface SeshSession {
  agent: string;
  id: string;
  title: string;
  summary?: string;
  slug?: string;
  created: string;
  last_used: string;
  directory?: string;
  resume_command: string;
  text?: string;
}
