"""Launch accounts + official Licentivo runtime; no local entitlement decisions."""
from .client import Application, APIError, MemoryStore, WindowsDPAPIStore

__version__ = "0.1.3"
__all__ = ["Application", "APIError", "MemoryStore", "WindowsDPAPIStore"]
