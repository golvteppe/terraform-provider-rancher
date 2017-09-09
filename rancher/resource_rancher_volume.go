package rancher

import (
	"fmt"
	"log"
	"time"

	"github.com/hashicorp/terraform/helper/resource"
	"github.com/hashicorp/terraform/helper/schema"
	rancherClient "github.com/rancher/go-rancher/v2"
)

func resourceRancherVolume() *schema.Resource {
	return &schema.Resource{
		Create: resourceRancherVolumeCreate,
		Read:   resourceRancherVolumeRead,
		Update: resourceRancherVolumeUpdate,
		Delete: resourceRancherVolumeDelete,
		Importer: &schema.ResourceImporter{
			State: resourceRancherVolumeImport,
		},

		Schema: map[string]*schema.Schema{
			"id": &schema.Schema{
				Type:     schema.TypeString,
				Computed: true,
			},
			"name": &schema.Schema{
				Type:     schema.TypeString,
				Required: true,
			},
			"driver": &schema.Schema{
				Type:     schema.TypeString,
				Required: true,
			},
			"description": &schema.Schema{
				Type:     schema.TypeString,
				Optional: true,
			},
			"environment_id": &schema.Schema{
				Type:     schema.TypeString,
				Required: true,
				ForceNew: true,
			},
		},
	}
}

func resourceRancherVolumeCreate(d *schema.ResourceData, meta interface{}) error {
	log.Printf("[INFO] Creating Volume: %s", d.Id())
	client, err := meta.(*Config).EnvironmentClient(d.Get("environment_id").(string))
	if err != nil {
		return err
	}

	name := d.Get("name").(string)
	description := d.Get("description").(string)
	driver := d.Get("driver").(string)

	volume := rancherClient.Volume{
		Name:        name,
		Description: description,
		Driver:      driver,
	}
	newVolume, err := client.Volume.Create(&volume)
	if err != nil {
		return err
	}

	stateConf := &resource.StateChangeConf{
		Pending:    []string{"inactive"},
		Target:     []string{"inactive"},
		Refresh:    VolumeStateRefreshFunc(client, newVolume.Id),
		Timeout:    10 * time.Minute,
		Delay:      1 * time.Second,
		MinTimeout: 3 * time.Second,
	}
	_, waitErr := stateConf.WaitForState()
	if waitErr != nil {
		return fmt.Errorf(
			"Error waiting for volume (%s) to be created: %s", newVolume.Id, waitErr)
	}

	d.SetId(newVolume.Id)
	log.Printf("[INFO] Volume ID: %s", d.Id())

	return resourceRancherVolumeRead(d, meta)
}

func resourceRancherVolumeRead(d *schema.ResourceData, meta interface{}) error {
	log.Printf("[INFO] Refreshing Volume: %s", d.Id())
	client, err := meta.(*Config).EnvironmentClient(d.Get("environment_id").(string))
	if err != nil {
		return err
	}

	volume, err := client.Volume.ById(d.Id())
	if err != nil {
		return err
	}

	if volume == nil {
		log.Printf("[INFO] Volume %s not found", d.Id())
		d.SetId("")
		return nil
	}

	if removed(volume.State) {
		log.Printf("[INFO] Volume %s was removed on %v", d.Id(), volume.Removed)
		d.SetId("")
		return nil
	}

	log.Printf("[INFO] Volume Name: %s", volume.Name)

	d.Set("description", volume.Description)
	d.Set("name", volume.Name)
	d.Set("driver", volume.Driver)
	d.Set("environment_id", volume.AccountId)

	return nil
}

func resourceRancherVolumeUpdate(d *schema.ResourceData, meta interface{}) error {
	client, err := meta.(*Config).EnvironmentClient(d.Get("environment_id").(string))
	if err != nil {
		return err
	}

	volume, err := client.Volume.ById(d.Id())
	if err != nil {
		return err
	}

	name := d.Get("name").(string)
	description := d.Get("description").(string)

	volume.Name = name
	volume.Description = description
	client.Volume.Update(volume, &volume)

	return resourceRancherVolumeRead(d, meta)
}

func resourceRancherVolumeDelete(d *schema.ResourceData, meta interface{}) error {
	log.Printf("[INFO] Deleting Volume: %s", d.Id())
	id := d.Id()
	client, err := meta.(*Config).EnvironmentClient(d.Get("environment_id").(string))
	if err != nil {
		return err
	}

	volume, err := client.Volume.ById(d.Id())
	if err != nil {
		return err
	}

	log.Printf("[DEBUG] Waiting for volume (%s) to be detached or inactive", id)

	stateConf := &resource.StateChangeConf{
		Pending:    []string{"active", "deactivating"},
		Target:     []string{"inactive", "detached"},
		Refresh:    VolumeStateRefreshFunc(client, id),
		Timeout:    10 * time.Minute,
		Delay:      1 * time.Second,
		MinTimeout: 3 * time.Second,
	}

	_, waitErr := stateConf.WaitForState()
	if waitErr != nil {
		return fmt.Errorf(
			"Error waiting for volume (%s) to be detached or inactive: %s", id, waitErr)
	}

	// Update resource to reflect its state
	volume, err = client.Volume.ById(id)
	if err != nil {
		return fmt.Errorf("Failed to refresh state of detached or inactive volume (%s): %s", id, err)
	}

	if _, err := client.Volume.ActionRemove(volume); err != nil {
		return fmt.Errorf("Error removing volume: %s", err)
	}

	log.Printf("[DEBUG] Waiting for volume (%s) to be removed", id)

	stateConf = &resource.StateChangeConf{
		Pending:    []string{"inactive", "detached", "removed", "removing"},
		Target:     []string{"removed"},
		Refresh:    VolumeStateRefreshFunc(client, id),
		Timeout:    10 * time.Minute,
		Delay:      1 * time.Second,
		MinTimeout: 3 * time.Second,
	}

	_, waitErr = stateConf.WaitForState()
	if waitErr != nil {
		return fmt.Errorf(
			"Error waiting for volume (%s) to be removed: %s", id, waitErr)
	}

	d.SetId("")
	return nil
}

func resourceRancherVolumeImport(d *schema.ResourceData, meta interface{}) ([]*schema.ResourceData, error) {
	envID, resourceID := splitID(d.Id())
	d.SetId(resourceID)
	if envID != "" {
		d.Set("environment_id", envID)
	} else {
		client, err := meta.(*Config).GlobalClient()
		if err != nil {
			return []*schema.ResourceData{}, err
		}
		volume, err := client.Volume.ById(d.Id())
		if err != nil {
			return []*schema.ResourceData{}, err
		}
		d.Set("environment_id", volume.AccountId)
	}
	return []*schema.ResourceData{d}, nil
}

// VolumeStateRefreshFunc returns a resource.StateRefreshFunc that is used to watch
// a Rancher Volume.
func VolumeStateRefreshFunc(client *rancherClient.RancherClient, volumeID string) resource.StateRefreshFunc {
	return func() (interface{}, string, error) {
		env, err := client.Volume.ById(volumeID)

		if err != nil {
			return nil, "", err
		}

		return env, env.State, nil
	}
}
