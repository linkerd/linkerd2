package multus

// func deleteMultusNetAttach(ctx context.Context, multusRef client.ObjectKey) error {
// 	logger := log.FromContext(ctx).WithValues(
// 		"k8s.cni.cncf.io/v1/NetworkAttachmentDefinition",
// 		multusRef.Namespace+"/"+multusRef.Name)

// 	logger.Info("Deleting Multus NetworkAttachmentDefinition")

// 	var multusNetAttach = &multusapi.NetworkAttachmentDefinition{}

// 	// Get current Multus NetworkAttachmentDefinition state.
// 	if err := r.Get(ctx, multusRef, multusNetAttach); err != nil {
// 		// Already deleted, nothing to do.
// 		if apierrors.IsNotFound(err) {
// 			logger.Info("Object has been deleted earlier")

// 			return nil
// 		}

// 		logger.Error(err, "GET error")

// 		return err
// 	}

// 	if err := r.Delete(ctx, multusNetAttach); err != nil {
// 		// Already deleted, nothing to do.
// 		if apierrors.IsNotFound(err) {
// 			logger.Info("Object has been deleted earlier")

// 			return nil
// 		}

// 		logger.Error(err, "Delete error")

// 		return err
// 	}

// 	return nil
// }
