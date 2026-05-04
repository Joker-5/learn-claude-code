"use client";

import { useMemo, useState, useRef, useEffect } from "react";
import { cn } from "@/lib/utils";

interface ClassItem {
  name: string;
  startLine: number;
  endLine: number;
}

interface FuncItem {
  name: string;
  signature: string;
  startLine: number;
}

interface SourceViewerProps {
  source: string;
  filename: string;
  classes: ClassItem[];
  functions: FuncItem[];
}

function highlightLine(line: string): React.ReactNode[] {
  const trimmed = line.trimStart();
  if (trimmed.startsWith("#")) {
    return [
      <span key={0} className="text-zinc-400 italic">
        {line}
      </span>,
    ];
  }
  if (trimmed.startsWith("@")) {
    return [
      <span key={0} className="text-amber-400">
        {line}
      </span>,
    ];
  }
  if (trimmed.startsWith('"""') || trimmed.startsWith("'''")) {
    return [
      <span key={0} className="text-emerald-500">
        {line}
      </span>,
    ];
  }

  const keywordSet = new Set([
    "def", "class", "import", "from", "return", "if", "elif", "else",
    "while", "for", "in", "not", "and", "or", "is", "None", "True",
    "False", "try", "except", "raise", "with", "as", "yield", "break",
    "continue", "pass", "global", "lambda", "async", "await",
  ]);

  const parts = line.split(
    /(\b(?:def|class|import|from|return|if|elif|else|while|for|in|not|and|or|is|None|True|False|try|except|raise|with|as|yield|break|continue|pass|global|lambda|async|await|self)\b|"(?:[^"\\]|\\.)*"|'(?:[^'\\]|\\.)*'|f"(?:[^"\\]|\\.)*"|f'(?:[^'\\]|\\.)*'|#.*$|\b\d+(?:\.\d+)?\b)/
  );

  return parts.map((part, idx) => {
    if (!part) return null;
    if (keywordSet.has(part)) {
      return <span key={idx} className="text-blue-400 font-medium">{part}</span>;
    }
    if (part === "self") {
      return <span key={idx} className="text-purple-400">{part}</span>;
    }
    if (part.startsWith("#")) {
      return <span key={idx} className="text-zinc-400 italic">{part}</span>;
    }
    if (
      (part.startsWith('"') && part.endsWith('"')) ||
      (part.startsWith("'") && part.endsWith("'")) ||
      (part.startsWith('f"') && part.endsWith('"')) ||
      (part.startsWith("f'") && part.endsWith("'"))
    ) {
      return <span key={idx} className="text-emerald-500">{part}</span>;
    }
    if (/^\d+(?:\.\d+)?$/.test(part)) {
      return <span key={idx} className="text-orange-400">{part}</span>;
    }
    return <span key={idx}>{part}</span>;
  });
}

type NamedItem = (ClassItem | FuncItem) & { kind: "class" | "function" };

export function SourceViewer({ source, filename, classes, functions }: SourceViewerProps) {
  const [selected, setSelected] = useState<NamedItem | null>(null);
  const lineRefs = useRef<Map<number, HTMLDivElement>>(new Map());

  const lines = useMemo(() => source.split("\n"), [source]);

  const allItems = useMemo<NamedItem[]>(() => {
    const items: NamedItem[] = [
      ...classes.map((c) => ({ ...c, kind: "class" as const })),
      ...functions.map((f) => ({ ...f, kind: "function" as const })),
    ];
    items.sort((a, b) => a.startLine - b.startLine);
    return items;
  }, [classes, functions]);

  const getEndLine = (item: NamedItem): number => {
    if ("endLine" in item) return item.endLine;
    const idx = allItems.findIndex((i) => i.startLine === item.startLine && i.kind === item.kind);
    if (idx < allItems.length - 1) {
      return allItems[idx + 1].startLine - 1;
    }
    return lines.length;
  };

  const selectedRange = useMemo<[number, number] | null>(() => {
    if (!selected) return null;
    return [selected.startLine - 1, getEndLine(selected) - 1];
  }, [selected, allItems, lines.length]);

  useEffect(() => {
    if (selected && selectedRange) {
      const el = lineRefs.current.get(selected.startLine - 1);
      if (el) {
        el.scrollIntoView({ block: "center", behavior: "smooth" });
      }
    }
  }, [selected]);

  const selectedSource = useMemo(() => {
    if (!selected || !selectedRange) return "";
    const [start, end] = selectedRange;
    return lines.slice(start, end + 1).join("\n");
  }, [selected, selectedRange, lines]);

  return (
    <div className="rounded-lg border border-zinc-200 dark:border-zinc-700">
      <div className="flex items-center gap-2 border-b border-zinc-200 px-4 py-2 dark:border-zinc-700">
        <div className="flex gap-1.5">
          <span className="h-3 w-3 rounded-full bg-red-400" />
          <span className="h-3 w-3 rounded-full bg-yellow-400" />
          <span className="h-3 w-3 rounded-full bg-green-400" />
        </div>
        <span className="font-mono text-xs text-zinc-400">{filename}</span>
      </div>

      <div className="flex max-h-[600px] flex-col lg:flex-row">
        {/* Left panel: method/class list */}
        <div className="w-full border-b border-zinc-200 lg:w-56 lg:border-b-0 lg:border-r dark:border-zinc-700">
          <div className="sticky top-0 max-h-[200px] overflow-y-auto p-3 lg:max-h-[600px]">
            {classes.length > 0 && (
              <div className="mb-3">
                <div className="mb-1 px-1 text-[10px] font-semibold uppercase tracking-wider text-zinc-400">
                  Classes
                </div>
                {classes.map((cls) => {
                  const item: NamedItem = { ...cls, kind: "class" };
                  return (
                    <button
                      key={cls.name}
                      onClick={() => setSelected(selected?.startLine === cls.startLine && selected?.kind === "class" ? null : item)}
                      className={cn(
                        "mb-0.5 flex w-full items-center gap-1.5 rounded px-2 py-1 text-left font-mono text-xs transition-colors",
                        selected?.startLine === cls.startLine && selected?.kind === "class"
                          ? "bg-blue-500/20 text-blue-400 dark:bg-blue-500/20"
                          : "text-zinc-300 hover:bg-zinc-700/50"
                      )}
                    >
                      <span className="text-[9px] text-zinc-500">{cls.startLine}</span>
                      <span className="truncate">{cls.name}</span>
                    </button>
                  );
                })}
              </div>
            )}

            {functions.length > 0 && (
              <div>
                <div className="mb-1 px-1 text-[10px] font-semibold uppercase tracking-wider text-zinc-400">
                  Functions
                </div>
                {functions.map((fn) => {
                  const item: NamedItem = { ...fn, kind: "function" };
                  return (
                    <button
                      key={fn.name}
                      onClick={() => setSelected(selected?.startLine === fn.startLine && selected?.kind === "function" ? null : item)}
                      className={cn(
                        "mb-0.5 flex w-full items-center gap-1.5 rounded px-2 py-1 text-left font-mono text-xs transition-colors",
                        selected?.startLine === fn.startLine && selected?.kind === "function"
                          ? "bg-blue-500/20 text-blue-400 dark:bg-blue-500/20"
                          : "text-zinc-300 hover:bg-zinc-700/50"
                      )}
                    >
                      <span className="text-[9px] text-zinc-500">{fn.startLine}</span>
                      <span className="truncate">{fn.name}</span>
                    </button>
                  );
                })}
              </div>
            )}

            {/* Inline detail panel */}
            {selected && (
              <div className="mt-3 border-t border-zinc-700 pt-3">
                <div className="mb-1 text-[10px] font-semibold text-zinc-400">
                  {selected.kind === "class" ? "Class" : "Function"}
                </div>
                <div className="mb-2 font-mono text-xs text-blue-400">
                  {selected.kind === "class" ? `class ${selected.name}` : (selected as FuncItem & { kind: "function" }).signature}
                </div>
                <div className="rounded border border-zinc-700 bg-zinc-950">
                  <pre className="max-h-40 overflow-auto p-2 text-[9px] leading-3 text-zinc-300">
                    <code>
                      {selectedSource.split("\n").map((line, i) => (
                        <div key={i}>{highlightLine(line)}</div>
                      ))}
                    </code>
                  </pre>
                </div>
              </div>
            )}
          </div>
        </div>

        {/* Right panel: full source code */}
        <div className="min-w-0 flex-1 overflow-x-auto bg-zinc-950">
          <pre className="p-2 text-[10px] leading-4 sm:p-4 sm:text-xs sm:leading-5">
            <code>
              {lines.map((line, i) => {
                const isHighlighted =
                  selectedRange != null && i >= selectedRange[0] && i <= selectedRange[1];
                return (
                  <div
                    key={i}
                    ref={(el) => {
                      if (el) lineRefs.current.set(i, el);
                    }}
                    className={cn(
                      "flex transition-colors",
                      isHighlighted && "bg-yellow-500/10"
                    )}
                  >
                    <span className="mr-2 inline-block w-6 shrink-0 select-none text-right text-zinc-600 sm:mr-4 sm:w-8">
                      {i + 1}
                    </span>
                    <span className="text-zinc-200">
                      {highlightLine(line)}
                    </span>
                  </div>
                );
              })}
            </code>
          </pre>
        </div>
      </div>
    </div>
  );
}
