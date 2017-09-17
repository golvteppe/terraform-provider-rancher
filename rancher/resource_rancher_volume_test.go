package rancher

import (
	"fmt"
	"log"
	"testing"
	"time"

	"github.com/hashicorp/terraform/helper/resource"
	"github.com/hashicorp/terraform/terraform"
	rancherClient "github.com/rancher/go-rancher/v2"
)

func TestAccRancherVolume_basic(t *testing.T) {
	var volume rancherClient.Volume

	resource.Test(t, resource.TestCase{
		PreCheck:     func() { testAccPreCheck(t) },
		Providers:    testAccProviders,
		CheckDestroy: testAccCheckRancherVolumeDestroy,
		Steps: []resource.TestStep{
			resource.TestStep{
				Config: testAccRancherVolumeConfig,
				Check: resource.ComposeTestCheckFunc(
					testAccCheckRancherVolumeExists("rancher_volume.foo", &volume),
					resource.TestCheckResourceAttr("rancher_volume.foo", "name", "foo"),
					resource.TestCheckResourceAttr("rancher_volume.foo", "description", "volume test"),
					resource.TestCheckResourceAttr("rancher_volume.foo", "driver", "rancher-nfs"),
				),
			},
			resource.TestStep{
				Config: testAccRancherVolumeUpdateConfig,
				Check: resource.ComposeTestCheckFunc(
					testAccCheckRancherVolumeExists("rancher_volume.foo", &volume),
					resource.TestCheckResourceAttr("rancher_volume.foo", "name", "foo2"),
					resource.TestCheckResourceAttr("rancher_volume.foo", "description", "registry test - updated"),
					resource.TestCheckResourceAttr("rancher_volume.foo", "driver", "rancher-ebs"),
				),
			},
			resource.TestStep{
				Config: testAccRancherVolumeRecreateConfig,
				Check: resource.ComposeTestCheckFunc(
					testAccCheckRancherVolumeExists("rancher_volume.foo", &volume),
					resource.TestCheckResourceAttr("rancher_volume.foo", "name", "foo"),
					resource.TestCheckResourceAttr("rancher_volume.foo", "description", "volume test"),
					resource.TestCheckResourceAttr("rancher_volume.foo", "server_address", "rancher-nfs"),
				),
			},
		},
	})
}

func TestAccRancherVolume_disappears(t *testing.T) {
	var volume rancherClient.Volume

	resource.Test(t, resource.TestCase{
		PreCheck:     func() { testAccPreCheck(t) },
		Providers:    testAccProviders,
		CheckDestroy: testAccCheckRancherVolumeDestroy,
		Steps: []resource.TestStep{
			resource.TestStep{
				Config: testAccRancherVolumeConfig,
				Check: resource.ComposeTestCheckFunc(
					testAccCheckRancherVolumeExists("rancher_volume.foo", &volume),
					testAccRancherVolumeDisappears(&volume),
				),
				ExpectNonEmptyPlan: true,
			},
		},
	})
}

func testAccRancherVolumeDisappears(vol *rancherClient.Volume) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		client, err := testAccProvider.Meta().(*Config).EnvironmentClient(vol.AccountId)
		if err != nil {
			return err
		}

		log.Printf("[DEBUG] Waiting for volume (%s) to be detached or inactive", vol.Id)

		stateConf := &resource.StateChangeConf{
			Pending:    []string{"active", "deactivating"},
			Target:     []string{"inactive", "detached"},
			Refresh:    VolumeStateRefreshFunc(client, vol.Id),
			Timeout:    10 * time.Minute,
			Delay:      1 * time.Second,
			MinTimeout: 3 * time.Second,
		}

		_, waitErr := stateConf.WaitForState()
		if waitErr != nil {
			return fmt.Errorf(
				"Error waiting for volume (%s) to be deactivated: %s", vol.Id, waitErr)
		}

		// Update resource to reflect its state
		vol, err = client.Volume.ById(vol.Id)
		if err != nil {
			return fmt.Errorf("Failed to refresh state of deactivated volume (%s): %s", vol.Id, err)
		}

		// Step 2: Remove
		if _, err := client.Volume.ActionRemove(vol); err != nil {
			return fmt.Errorf("Error removing volume: %s", err)
		}

		stateConf = &resource.StateChangeConf{
			Pending:    []string{"inactive", "detached", "removing"},
			Target:     []string{"removed"},
			Refresh:    VolumeStateRefreshFunc(client, vol.Id),
			Timeout:    10 * time.Minute,
			Delay:      1 * time.Second,
			MinTimeout: 3 * time.Second,
		}

		_, waitErr = stateConf.WaitForState()
		if waitErr != nil {
			return fmt.Errorf(
				"Error waiting for volume (%s) to be removed: %s", vol.Id, waitErr)
		}

		return nil
	}
}

func testAccCheckRancherVolumeExists(n string, vol *rancherClient.Volume) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[n]

		if !ok {
			return fmt.Errorf("Not found: %s", n)
		}

		if rs.Primary.ID == "" {
			return fmt.Errorf("No App Name is set")
		}

		client, err := testAccProvider.Meta().(*Config).EnvironmentClient(rs.Primary.Attributes["environment_id"])
		if err != nil {
			return err
		}

		foundVol, err := client.Volume.ById(rs.Primary.ID)
		if err != nil {
			return err
		}

		if foundVol.Resource.Id != rs.Primary.ID {
			return fmt.Errorf("Volume not found")
		}

		*vol = *foundVol

		return nil
	}
}

func testAccCheckRancherVolumeDestroy(s *terraform.State) error {
	for _, rs := range s.RootModule().Resources {
		if rs.Type != "rancher_volume" {
			continue
		}
		client, err := testAccProvider.Meta().(*Config).GlobalClient()
		if err != nil {
			return err
		}

		vol, err := client.Volume.ById(rs.Primary.ID)

		if err == nil {
			if vol != nil &&
				vol.Resource.Id == rs.Primary.ID &&
				vol.State != "removed" {
				return fmt.Errorf("Volume still exists")
			}
		}

		return nil
	}
	return nil
}

const testAccRancherVolumeConfig = `
resource "rancher_environment" "foo_volume" {
	name = "volume test"
	description = "environment to test volumes"
	orchestration = "cattle"
}

resource "rancher_volume" "foo" {
  name = "foo"
  description = "volume test"
  driver = "rancher-nfs"
  environment_id = "${rancher_environment.foo_volume.id}"
}
`

const testAccRancherVolumeUpdateConfig = `
 resource "rancher_environment" "foo_volume" {
   name = "volume test"
   description = "environment to test volumes"
   orchestration = "cattle"
 }

 resource "rancher_registry" "foo" {
   name = "foo2"
   description = "volume test - updated"
   driver = "rancher-ebs"
   environment_id = "${rancher_environment.foo_volume.id}"
 }
 `

const testAccRancherVolumeRecreateConfig = `
 resource "rancher_environment" "foo_volume" {
   name = "volume test"
   description = "environment to test volumes"
   orchestration = "cattle"
 }

 resource "rancher_environment" "foo_volume2" {
   name = "alternative volume test"
   description = "other environment to test volumes"
 }

 resource "rancher_volume" "foo" {
   name = "foo"
   description = "volume test"
   driver = "rancher-nfs"
   environment_id = "${rancher_environment.foo_volume2.id}"
 }
 `
