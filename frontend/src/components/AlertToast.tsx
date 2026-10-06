import { Link } from 'react-router-dom'
import { toast } from 'sonner'
import { useWS } from '@/lib/ws'

export default function AlertToast() {
	useWS(event => {
		if (event.type !== 'alert') return
		const p = event.payload
		toast.custom(id => <div className="min-w-[260px] rounded-lg border border-accent bg-accent-bg px-3.5 py-2.5 shadow-[0_2px_12px_rgba(0,0,0,0.18)]">
			<div className="font-mono text-[10px] font-bold uppercase tracking-[.07em] text-accent-ink">Alert {p.state}</div>
			<div className="mt-1 font-mono text-[11px] text-ink2">{p.name ?? 'A telemetry alert changed state.'}</div>
			<Link to="/alerts" onClick={() => toast.dismiss(id)} className="mt-1.5 block font-mono text-[10px] text-accent-ink underline decoration-dotted">open alerts →</Link>
		</div>, { duration: 5000 })
	})
	return null
}
