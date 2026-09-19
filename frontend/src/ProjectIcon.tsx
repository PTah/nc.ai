/** Default project glyph when the folder has no app/project icon. */
export function DefaultProjectIcon() {
  return (
    <span className="nc-project-icon nc-project-icon-default" aria-hidden>
      <svg viewBox="0 0 20 20" width="14" height="14" fill="none" xmlns="http://www.w3.org/2000/svg">
        {/* Soft folder + AI spark — “workspace the agent works in” */}
        <path
          d="M2.5 6.25h5.1l1.15 1.2H17.5v8.3a1.25 1.25 0 0 1-1.25 1.25H3.75A1.25 1.25 0 0 1 2.5 15.75V6.25Z"
          stroke="currentColor"
          strokeWidth="1.35"
          strokeLinejoin="round"
        />
        <path
          d="M2.5 8.6h15"
          stroke="currentColor"
          strokeWidth="1.35"
          strokeLinecap="round"
          opacity="0.55"
        />
        <path
          d="M13.2 11.1l.55 1.35 1.4.55-1.4.55-.55 1.35-.55-1.35-1.4-.55 1.4-.55.55-1.35Z"
          fill="currentColor"
        />
      </svg>
    </span>
  )
}

type Props = {
  src?: string
  name: string
}

/** Project list icon: folder asset when present, otherwise the default glyph. */
export default function ProjectIcon({src, name}: Props) {
  if (src) {
    return (
      <img
        className="nc-project-icon"
        src={src}
        alt=""
        title={name}
        draggable={false}
        loading="lazy"
      />
    )
  }
  return <DefaultProjectIcon />
}
