package station

import (
	"context"

	"example.com/paleo-prep-station/internal/contracts"
	"example.com/paleo-prep-station/internal/domain"
	"example.com/paleo-prep-station/internal/repository"
)

func (s *Service) RecordInspection(
	ctx context.Context,
	planID string,
	request contracts.InspectionRequest,
) (domain.Inspection, error) {
	if err := ctx.Err(); err != nil {
		return domain.Inspection{}, err
	}

	var created domain.Inspection
	err := s.repo.Mutate(func(m *repository.Mutation) error {
		plan, ok := m.GetPlan(planID)
		if !ok {
			return notFoundPlan(planID)
		}
		inspection, err := domain.NewInspection(
			s.nextID("inspection"),
			plan,
			request.CheckpointKey,
			request.Result,
			request.InspectedBy,
			request.Note,
			m.InspectionsForPlan(planID),
			s.now(),
		)
		if err != nil {
			return err
		}
		m.AppendInspection(inspection)
		created = inspection
		return nil
	})
	return created, err
}
