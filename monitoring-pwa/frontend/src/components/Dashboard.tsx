import { Server, ChevronRight, LayoutDashboard, Skull } from 'lucide-react'

interface DashboardProps {
	grafanaBase: string
}

// Dashboard コンポーネントである。ノード健全性グラフおよびGrafanaリンクを表示する。
export default function Dashboard({ grafanaBase }: DashboardProps) {
	return (
		<>
			<div className="card card-flush">
				<div className="panel-header">
					<h2 className="panel-title">
						<Server size={14} />
						ノード健全性
					</h2>
					<a
						href={`${grafanaBase}/grafana/d/nuage-node-health/node-health-overview?orgId=1`}
						target="_blank"
						rel="noopener noreferrer"
						className="panel-action"
					>
						Grafanaで開く
						<ChevronRight size={12} />
					</a>
				</div>
				<iframe
					src={`${grafanaBase}/grafana/d/nuage-node-health/node-health-overview?orgId=1&kiosk&theme=dark`}
					height="420"
					className="grafana-embed"
					title="Grafana Node Health Dashboard"
				></iframe>
			</div>

			<a href={`${grafanaBase}/grafana`} target="_blank" rel="noopener noreferrer" className="card link-card">
				<h2 className="card-title">
					<LayoutDashboard size={16} />
					Grafana
				</h2>
				<ChevronRight size={16} className="link-card-arrow" />
			</a>

			<a href={`${grafanaBase}/chaos-monitor`} target="_blank" rel="noopener noreferrer" className="card link-card">
				<h2 className="card-title">
					<Skull size={16} />
					Chaos Monitor
				</h2>
				<ChevronRight size={16} className="link-card-arrow" />
			</a>
		</>
	)
}
