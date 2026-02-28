import { useCallback, useState } from "react";

interface Props {
  code: string;
}

export default function CodeBlock({ code }: Props) {
  const [copied, setCopied] = useState(false);

  const handleCopy = useCallback(() => {
    navigator.clipboard.writeText(code).then(() => {
      setCopied(true);
      setTimeout(() => setCopied(false), 1200);
    });
  }, [code]);

  return (
    <div className="relative overflow-hidden rounded-xl border border-line-soft bg-ink">
      <pre className="overflow-x-auto px-4 py-3 font-mono text-[12px] leading-relaxed text-text">
        <code>
          <span className="select-none text-teal">$ </span>
          {code}
        </code>
      </pre>
      <button
        onClick={handleCopy}
        className="absolute right-2 top-2 rounded-full border border-line px-2.5 py-1 font-mono text-[10px] uppercase tracking-[0.08em] text-muted transition-colors hover:border-teal hover:text-teal"
      >
        {copied ? "Copied" : "Copy"}
      </button>
    </div>
  );
}
