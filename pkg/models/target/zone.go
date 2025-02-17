package target

import (
	"errors"
	"fmt"
	"os"
	"path"
	"strings"

	"log"

	"github.com/bougou/gopkg/common"
	"github.com/bougou/sail/pkg/ansible"
	"github.com/bougou/sail/pkg/models"
	"github.com/bougou/sail/pkg/models/cmdb"
	"github.com/bougou/sail/pkg/models/product"
	"gopkg.in/yaml.v3"
)

const (
	SailMetaVarProduct  = "_sail_product"
	SailMetaVarHelmMode = "_sail_helm_mode"

	SailHelmModeComponent = "component"
	SailHelmModeProduct   = "product"

	SailPlaybookFile = ".sail.yaml"
)

type ZoneMeta struct {
	// tag value must equal to SailMetaVarProduct
	SailProduct string `json:"_sail_product" yaml:"_sail_product"`

	// tag value must equal to SailMetaVarHelmMode
	SailHelmMode string `json:"_sail_helm_mode" yaml:"_sail_helm_mode"`
}

type Zone struct {
	*ZoneMeta

	TargetName    string
	ZoneName      string
	TargetDir     string
	ZoneDir       string
	VarsFile      string
	HostsFile     string
	PlatformsFile string
	ComputedFile  string

	ResourcesDir string

	HelmDir string

	Product    *product.Product
	CMDB       *cmdb.CMDB
	TargetVars *TargetVars

	ansibleCfgFile string

	sailOption *models.SailOption
}

func NewZone(sailOption *models.SailOption, targetName string, zoneName string) *Zone {
	zone := &Zone{
		TargetName: targetName,
		ZoneName:   zoneName,

		TargetDir: path.Join(sailOption.TargetsDir, targetName),
		ZoneDir:   path.Join(sailOption.TargetsDir, targetName, zoneName),

		VarsFile:      path.Join(sailOption.TargetsDir, targetName, zoneName, "vars.yaml"),
		HostsFile:     path.Join(sailOption.TargetsDir, targetName, zoneName, "hosts.yaml"),
		PlatformsFile: path.Join(sailOption.TargetsDir, targetName, zoneName, "platforms.yaml"),
		ComputedFile:  path.Join(sailOption.TargetsDir, targetName, zoneName, "_computed.yaml"),

		ResourcesDir: path.Join(sailOption.TargetsDir, targetName, zoneName, "resources"),

		HelmDir: path.Join(sailOption.TargetsDir, targetName, zoneName, "helm"),

		CMDB:       cmdb.NewCMDB(),
		TargetVars: NewTargetVars(),

		ansibleCfgFile: path.Join(sailOption.ProductsDir, "ansible.cfg"),

		sailOption: sailOption,
	}

	return zone
}

// LoadNew fill vars to zone. The zone is treated as a newly created zone.
// So it will ONLY load default varibles from product.
// This method is ONLY called when `conf-create`.
func (zone *Zone) LoadNew() error {
	// for newly created zone, the zone.ProductName is set by conf-create
	if zone.SailProduct == "" {
		return errors.New("empty product name")
	}
	p := product.NewProduct(zone.SailProduct, zone.sailOption.ProductsDir)
	if err := p.Init(); err != nil {
		return fmt.Errorf("init product failed, err: %s", err)
	}

	zone.Product = p

	// fill zone meta vars
	zone.Product.Vars[SailMetaVarProduct] = zone.SailProduct
	zone.Product.Vars[SailMetaVarHelmMode] = zone.SailHelmMode

	return nil
}

// Load initialize the zone. The zone is supposed to be already exists.
// It will try to determine the product name from zone vars file.
func (zone *Zone) Load() error {
	zoneMeta, err := zone.ParseZoneMeta()
	if err != nil {
		return fmt.Errorf("parse zone meta failed, err: %s", err)
	}
	zone.ZoneMeta = zoneMeta

	if zone.SailProduct == "" {
		return errors.New("empty product name")
	}

	p := product.NewProduct(zone.SailProduct, zone.sailOption.ProductsDir)
	if err := p.Init(); err != nil {
		return fmt.Errorf("init product failed, err: %s", err)
	}

	if err := zone.LoadHosts(); err != nil {
		return fmt.Errorf("load hosts failed, err: %s", err)
	}

	if err := zone.LoadPlatforms(); err != nil {
		return fmt.Errorf("load platforms failed, err: %s", err)
	}

	if err := p.LoadZone(zone.VarsFile); err != nil {
		return fmt.Errorf("load zone vars failed, err: %s", err)
	}

	zone.Product = p

	if err := zone.PrepareHelm(); err != nil {
		return fmt.Errorf("prepare helm failed, err: %s", err)
	}

	return nil
}

func (zone *Zone) Compute() error {
	//  add or remove cmdb info for component according to whether the component is enabled
	for componentName, component := range zone.Product.Components {
		if err := zone.CMDB.Compute(componentName, component.Enabled); err != nil {
			return fmt.Errorf("compute cmdb for component (%s) failed, err: %s", componentName, err)
		}
	}

	// compute the "computed" fields for components of the product
	if err := zone.Product.Compute(zone.CMDB); err != nil {
		return fmt.Errorf("compute zone product failed, err: %s", err)
	}

	return nil
}

func (zone *Zone) LoadTarget() error {
	targetName := zone.TargetName
	target := NewTarget(zone.sailOption, targetName)
	if err := target.LoadAllZones(); err != nil {
		return fmt.Errorf("load all zones for target (%s) failed, err: %s", targetName, err)
	}

	zone.TargetVars = target.vars

	return nil
}

func (zone *Zone) ParseZoneMeta() (*ZoneMeta, error) {
	b, err := os.ReadFile(zone.VarsFile)
	if err != nil {
		return nil, fmt.Errorf("read zone vars file failed, err: %s", err)
	}

	m := &ZoneMeta{}
	if err := yaml.Unmarshal(b, &m); err != nil {
		return nil, fmt.Errorf("yaml unmarshal failed, err: %s", err)
	}

	if m.SailProduct == "" {
		return nil, fmt.Errorf("not found (%s) variable in %s, you have to fix that before continue", SailMetaVarProduct, zone.VarsFile)
	}

	return m, nil
}

func (zone *Zone) HandleCompatibity() {
	// Todo
	// Domain Specific Language (Declarative)
	// migrate.yaml
}

func (zone *Zone) Dump() error {
	if err := zone.Compute(); err != nil {
		return fmt.Errorf("zone compute failed, err: %s", err)
	}

	if err := os.MkdirAll(zone.ZoneDir, os.ModePerm); err != nil {
		return fmt.Errorf("make zone dir failed, err: %s", err)
	}

	if err := zone.LoadTarget(); err != nil {
		return fmt.Errorf("load target failed, err: %s", err)
	}

	errs := []string{}
	if err := zone.RenderSailPlaybook(); err != nil {
		errs = append(errs, err.Error())
	}
	if err := zone.RenderVars(); err != nil {
		errs = append(errs, err.Error())
	}
	if err := zone.RenderHosts(); err != nil {
		errs = append(errs, err.Error())
	}
	if err := zone.RenderPlatforms(); err != nil {
		errs = append(errs, err.Error())
	}
	if err := zone.RenderComputed(); err != nil {
		errs = append(errs, err.Error())
	}

	if len(errs) != 0 {
		return fmt.Errorf("%s", strings.Join(errs, "\n"))
	}
	return nil
}

// RenderSailPlaybook renders the default temporary ansible playbook file for the product of the zone.
func (zone *Zone) RenderSailPlaybook() error {
	product := zone.Product
	playbook, err := product.GenSail()
	if err != nil {
		msg := fmt.Sprintf("gen sail playbook failed, err: %s", err)
		return errors.New(msg)
	}

	b, err := common.Encode("yaml", playbook)
	if err != nil {
		fmt.Println("encode vars failed", err)
	}

	if err := os.WriteFile(zone.Product.SailPlaybookFile(), b, 0644); err != nil {
		fmt.Println("write product sail playbook file failed", err)
	}

	return nil
}

func (zone *Zone) RenderVars() error {
	m := make(map[string]interface{})

	for k, v := range zone.Product.Vars {
		m[k] = v
	}

	for k, v := range zone.Product.Components {
		m[k] = v
	}

	b, err := common.Encode("yaml", m)
	if err != nil {
		return fmt.Errorf("encode vars failed, err: %s", err)
	}

	if err := os.WriteFile(zone.VarsFile, b, 0644); err != nil {
		return fmt.Errorf("write vars file failed, err: %s", err)
	}

	return nil
}

func (zone *Zone) RenderHosts() error {
	b, err := common.Encode("yaml", zone.CMDB.Inventory)
	if err != nil {
		return fmt.Errorf("encode cmdb inventory failed, err: %s", err)
	}

	if err := os.WriteFile(zone.HostsFile, b, 0644); err != nil {
		return fmt.Errorf("write hosts file failed, err: %s", err)
	}

	return nil
}

func (zone *Zone) RenderPlatforms() error {
	b, err := common.Encode("yaml", zone.CMDB.Platforms)
	if err != nil {
		return fmt.Errorf("encode cmdb platforms failed, err: %s", err)
	}

	if err := os.WriteFile(zone.PlatformsFile, b, 0644); err != nil {
		return fmt.Errorf("write platforms file failed, err: %s", err)
	}

	return nil
}

func (zone *Zone) RenderComputed() error {
	m := make(map[string]interface{})
	m["inventory"] = zone.CMDB.Inventory
	m["platforms"] = zone.CMDB.Platforms
	m["targetvars"] = zone.TargetVars

	b, err := common.Encode("yaml", m)
	if err != nil {
		return fmt.Errorf("encode vars failed, err: %s", err)
	}

	if err := os.WriteFile(zone.ComputedFile, b, 0644); err != nil {
		return fmt.Errorf("write computed file failed, err: %s", err)
	}

	return nil
}

func (zone *Zone) PatchActionHostsMap(m map[string][]ansible.ActionHosts) error {
	for groupName, ahs := range m {
		if !zone.Product.HasComponent(groupName) && groupName != "_cluster" {
			return fmt.Errorf("not supported component in this product, supported components: %s", zone.Product.ComponentList())
		}

		for _, ah := range ahs {
			zone.PatchActionHosts(groupName, &ah)
		}
	}

	return nil
}

func (zone *Zone) PatchActionHosts(groupName string, hostsPatch *ansible.ActionHosts) {
	if zone.CMDB.Inventory.HasGroup(groupName) {
		group, _ := zone.CMDB.Inventory.GetGroup(groupName)
		ansible.PatchAnsibleGroup(group, hostsPatch)
	} else {
		if hostsPatch.Action == "delete" {
			return
		}

		group := ansible.NewGroup(groupName)
		for _, host := range hostsPatch.Hosts {
			group.AddHost(host)
			group.SetHostVars(host, map[string]interface{}{})
		}
		zone.CMDB.Inventory.AddGroup(group)
	}
}

func (zone *Zone) BuildInventory(hostsMap map[string][]string) error {

	for k, v := range hostsMap {
		if !zone.Product.HasComponent(k) {
			log.Printf("%s is not valid components, omit, valid components: %v\n", k, zone.Product.ComponentList())
			continue
		}

		group := ansible.NewGroup(k)
		for _, host := range v {
			group.AddHost(host)
			group.SetHostVars(host, map[string]interface{}{})
		}

		zone.CMDB.Inventory.AddGroup(group)
	}

	return nil
}

func (zone *Zone) PlaybookFile(playbookName string) string {
	if playbookName == "" {
		// auto generated when sail runs
		return path.Join(zone.Product.Dir, product.DefaultPlaybookFile)
	}

	if strings.HasSuffix(playbookName, ".yaml") {
		return path.Join(zone.Product.Dir, playbookName)
	}

	f := path.Join(zone.Product.Dir, playbookName+".yaml")
	if _, err := os.Stat(f); !os.IsNotExist(err) {
		return f
	}

	return path.Join(zone.Product.Dir, product.DefaultPlaybookFile)
}

func (zone *Zone) SetComponentVersion(componentName string, componentVersion string) error {
	if !zone.Product.HasComponent(componentName) {
		return fmt.Errorf("zone does not have component: (%s)", componentName)
	}
	zone.Product.Components[componentName].Version = componentVersion
	return nil
}

func (zone *Zone) LoadHosts() error {
	b, err := os.ReadFile(zone.HostsFile)
	if err != nil {
		return fmt.Errorf("read file (%s) failed, err: %s", zone.HostsFile, err)
	}

	i := ansible.NewAnsibleInventory()
	if err := yaml.Unmarshal(b, i); err != nil {
		return fmt.Errorf("unmarshal hosts failed, err: %s", err)
	}

	zone.CMDB.Inventory = i
	return nil
}

func (zone *Zone) LoadPlatforms() error {
	i := map[string]cmdb.Platform{}

	b, err := os.ReadFile(zone.PlatformsFile)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			zone.CMDB.Platforms = i
			return nil
		}
		return fmt.Errorf("read file (%s) failed, err: %s", zone.PlatformsFile, err)
	}

	if err := yaml.Unmarshal(b, &i); err != nil {
		return fmt.Errorf("unmarshal platforms failed, err: %s", err)
	}

	zone.CMDB.Platforms = i
	return nil
}

func (zone *Zone) GetK8SForComponent(componentName string) *cmdb.K8S {
	if platform, ok := zone.CMDB.Platforms[componentName]; ok {
		if platform.K8S != nil {
			return platform.K8S
		}
	}

	if platform, ok := zone.CMDB.Platforms["all"]; ok {
		if platform.K8S != nil {
			return platform.K8S
		}
	}

	return &cmdb.K8S{}
}

func (zone *Zone) GetK8SForProduct() *cmdb.K8S {
	if platform, ok := zone.CMDB.Platforms["all"]; ok {
		if platform.K8S != nil {
			return platform.K8S
		}
	}

	return &cmdb.K8S{}
}
