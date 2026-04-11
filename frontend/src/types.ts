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
};

export type DirectoryPassword = {
  id: number;
  path: string;
  enabled: boolean;
  version: number;
  createdAt: string;
  updatedAt: string;
};

export type APIError = {
  code: string;
  message: string;
  requiredPaths?: string[];
};
