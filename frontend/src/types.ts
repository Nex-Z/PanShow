export type Role = "admin" | "user";

export type User = {
  id: number;
  username: string;
  role: Role;
  active: boolean;
  createdAt: string;
  updatedAt: string;
};

export type FileEntry = {
  name: string;
  path: string;
  isDir: boolean;
  size: number;
  lastModified?: string;
  contentType?: string;
  metadataUnavailable?: boolean;
};

export type DirectoryPassword = {
  id: number;
  path: string;
  enabled: boolean;
  version: number;
  createdAt: string;
  updatedAt: string;
};

export type Announcement = {
  id: number;
  title: string;
  pattern: string;
  content: string;
  enabled: boolean;
  sortOrder: number;
  createdAt: string;
  updatedAt: string;
};

export type PublicAnnouncement = Pick<Announcement, "id" | "title" | "content" | "sortOrder">;

export type APIError = {
  code: string;
  message: string;
  requiredPaths?: string[];
  announcements?: PublicAnnouncement[];
};
