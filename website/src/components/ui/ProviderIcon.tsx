import { useState } from "react";

interface Props {
  slug: string;
  name: string;
  size?: number;
}

export default function ProviderIcon({ slug, name, size = 16 }: Props) {
  const [failed, setFailed] = useState(false);

  if (failed) {
    return (
      <span
        className="inline-flex items-center justify-center text-dim"
        style={{ width: size, height: size, fontSize: size * 0.5 }}
      >
        {name[0]}
      </span>
    );
  }

  return (
    <img
      src={`https://cdn.simpleicons.org/${slug}/666666`}
      alt=""
      width={size}
      height={size}
      loading="lazy"
      onError={() => setFailed(true)}
      className="opacity-80"
    />
  );
}
