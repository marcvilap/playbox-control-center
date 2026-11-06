package descubrir

import (
	"fmt"
	"path/filepath"
	"time"

	"playbox/internal/ejec"
)

type script struct{ nombre, ps string }

var scriptsWindows = []script{
	{"sistema", "Get-ComputerInfo | Select OsName,OsVersion,OsBuildNumber,CsModel,CsTotalPhysicalMemory | ConvertTo-Json"},
	{"hotfix", "Get-HotFix | Sort InstalledOn -Desc | Select -First 20 HotFixID,Description,@{n='InstalledOn';e={$_.InstalledOn.ToString('yyyy-MM-dd')}} | ConvertTo-Json"},
	{"windows_update", "Get-WinEvent -LogName 'Microsoft-Windows-WindowsUpdateClient/Operational' -MaxEvents 60 | Select @{n='Fecha';e={$_.TimeCreated.ToString('s')}},Id,LevelDisplayName,Message | ConvertTo-Json"},
	{"errores_sistema_30d", "Get-WinEvent -FilterHashtable @{LogName='System';Level=1,2,3;StartTime=(Get-Date).AddDays(-30)} | Group ProviderName | Sort Count -Desc | Select -First 25 Count,Name | ConvertTo-Json"},
	{"eventos_pnp_7d", "Get-WinEvent -FilterHashtable @{LogName='Microsoft-Windows-Kernel-PnP/Configuration';StartTime=(Get-Date).AddDays(-7)} -MaxEvents 300 | Select @{n='Fecha';e={$_.TimeCreated.ToString('s')}},Id,Message | ConvertTo-Json"},
	{"usb_hid", "Get-PnpDevice -Class HIDClass,Mouse,Keyboard,USB | Select @{n='Status';e={\"$($_.Status)\"}},Class,FriendlyName,InstanceId | ConvertTo-Json"},
	{"red_adaptadores", "Get-NetAdapter | Select Name,Status,LinkSpeed,MacAddress,DriverVersion,@{n='DriverDate';e={$_.DriverDate.ToString('yyyy-MM-dd')}} | ConvertTo-Json"},
	{"gpu", "Get-CimInstance Win32_VideoController | Select Name,DriverVersion,@{n='DriverDate';e={$_.DriverDate.ToString('yyyy-MM-dd')}},AdapterRAM | ConvertTo-Json"},
	{"discos", "Get-PSDrive -PSProvider FileSystem | Select Name,Used,Free | ConvertTo-Json"},
	{"plan_energia", "powercfg /getactivescheme"},
	{"horas_activas_wu", "Get-ItemProperty 'HKLM:\\SOFTWARE\\Microsoft\\WindowsUpdate\\UX\\Settings' | Select ActiveHoursStart,ActiveHoursEnd,PauseUpdatesExpiryTime | ConvertTo-Json"},
}

// ResumenWindows resume el estado del PC.
type ResumenWindows struct {
	SO              string   `json:"so"`
	Actualizaciones []string `json:"actualizaciones_recientes"`
	Errores30d      []string `json:"top_errores_30d"`
	HIDProblemas    []string `json:"usb_hid_no_ok"`
}

func recogerWindows(out string) ResumenWindows {
	dir := filepath.Join(out, "1_windows")
	res := map[string]string{}
	for _, s := range scriptsWindows {
		_, txt := ejec.PS(2*time.Minute, s.ps)
		guardar(dir, s.nombre, txt)
		res[s.nombre] = txt
	}
	r := ResumenWindows{SO: "?"}
	if l := ComoLista(res["sistema"]); len(l) > 0 {
		r.SO = fmt.Sprintf("%s (build %s)", S(l[0], "OsName"), S(l[0], "OsBuildNumber"))
	}
	for i, h := range ComoLista(res["hotfix"]) {
		if i == 5 {
			break
		}
		r.Actualizaciones = append(r.Actualizaciones, fmt.Sprintf("%s (%s)", S(h, "HotFixID"), S(h, "InstalledOn")))
	}
	for i, e := range ComoLista(res["errores_sistema_30d"]) {
		if i == 8 {
			break
		}
		r.Errores30d = append(r.Errores30d, fmt.Sprintf("%s: %s", S(e, "Name"), S(e, "Count")))
	}
	for _, d := range ComoLista(res["usb_hid"]) {
		if st := S(d, "Status"); st != "" && st != "OK" && st != "Unknown" {
			r.HIDProblemas = append(r.HIDProblemas, fmt.Sprintf("%s [%s]", S(d, "FriendlyName"), st))
		}
	}
	return r
}
