"use client";

import Image from "next/image";
import { useState } from "react";

const DEFAULT = "/images/default-article.jpg";

interface Props {
  src: string;
  alt: string;
  fill?: boolean;
  sizes?: string;
  className?: string;
  priority?: boolean;
}

export function ArticleImage({ src, alt, fill, sizes, className, priority }: Props) {
  const [imgSrc, setImgSrc] = useState(src || DEFAULT);

  return (
    <Image
      src={imgSrc || DEFAULT}
      alt={alt}
      fill={fill}
      sizes={sizes}
      className={className}
      priority={priority}
      onError={() => setImgSrc(DEFAULT)}
    />
  );
}
