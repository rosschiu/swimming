import { useId, useState, type KeyboardEvent, type ReactNode } from 'react';

type Section = { id: string; label: string; content: ReactNode };

// Phones show one profile section at a time; wide screens keep every section on the page.
// Hidden sections stay mounted so drafts and in-flight booking state survive switching.
export default function ProfileSections({ compact, label, sections }: { compact: boolean; label: string; sections: Section[] }) {
  const id = useId();
  const [current, setCurrent] = useState(sections[0]?.id);
  if (!compact || sections.length < 2) return <>{sections.map(section => <div key={section.id}>{section.content}</div>)}</>;
  const active = sections.some(section => section.id === current) ? current : sections[0].id;
  function move(event: KeyboardEvent<HTMLButtonElement>, index: number) {
    const next = event.key === 'ArrowRight' ? index + 1 : event.key === 'ArrowLeft' ? index - 1 : null;
    if (next === null) return;
    event.preventDefault();
    const target = sections[(next + sections.length) % sections.length];
    setCurrent(target.id);
    document.getElementById(`${id}-tab-${target.id}`)?.focus();
  }
  return <>
    <div className="segments" role="tablist" aria-label={label}>
      {sections.map((section, index) => <button key={section.id} id={`${id}-tab-${section.id}`} type="button" role="tab"
        aria-selected={active === section.id} aria-controls={`${id}-panel-${section.id}`} tabIndex={active === section.id ? 0 : -1}
        className={active === section.id ? 'segment segment-active' : 'segment'} onClick={() => setCurrent(section.id)} onKeyDown={event => move(event, index)}>{section.label}</button>)}
    </div>
    {sections.map(section => <div key={section.id} id={`${id}-panel-${section.id}`} role="tabpanel" aria-labelledby={`${id}-tab-${section.id}`} hidden={active !== section.id}>{section.content}</div>)}
  </>;
}
